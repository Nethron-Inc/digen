package generator

import (
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

const (
	// providerSetType is the type a variable must have to be recognised as a
	// provider set, and the markers are then taken from the package declaring it.
	providerSetType = "ProviderSet"

	// loggerField is the field of the container configuration that generated
	// code writes its debug lines to.
	loggerField = "Logger"
	// loggerType is the type that field must have.
	loggerType = "*log/slog.Logger"

	eagerMarker = "Eager"
	asMarker    = "As"

	// generatedSuffix turns the file declaring a provider set into the file the
	// container is written to: providers.go becomes providers_gen.go.
	generatedSuffix = "_gen.go"
)

// provider is one entry of the provider set: a constructor whose parameters are
// its dependencies and whose first result is what it provides.
type provider struct {
	fn       *types.Func
	provides types.Type
	alsoAs   []types.Type // extra interfaces declared with digen.As
	deps     []types.Type
	variadic bool
	fallible bool // returns (T, error)
	eager    bool // declared with digen.Eager
	order    int  // position in the provider set, which fixes slice ordering
	name     string
}

// input is a value passed into the container constructor rather than built by it.
type input struct {
	name string // parameter name, also the container field
	typ  types.Type
}

// String identifies the provider in diagnostics.
func (p *provider) String() string {
	if pkg := p.fn.Pkg(); pkg != nil {
		return pkg.Name() + "." + p.fn.Name()
	}
	return p.fn.Name()
}

// set is one provider set found in the target package, along with the names and
// paths derived from its declaration.
type set struct {
	varName       string // the declared variable, always unexported
	containerType string // the exported spelling, which names the generated type
	containerCtor string // "New" + containerType
	outPath       string // the declaring file with _gen.go in place of .go
	elems         []ast.Expr
}

type loaded struct {
	pkg        *packages.Package
	modulePath string
	pkgName    string
	// pkgErrors are the target package's type errors. They are carried rather
	// than printed: most of them come from the stale generated file this run is
	// about to replace. See Generate for how they are reported.
	pkgErrors []packages.Error
	sets      []*loadedSet
}

// loadedSet is one set with its providers and container inputs resolved.
type loadedSet struct {
	set
	providers []*provider
	// diConfig is the container's own configuration parameter, always first.
	diConfig input
	// inputs are the remaining injected values.
	inputs []input
}

func load(dir, pkgPattern, tag, varFilter string) (*loaded, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps |
			packages.NeedImports | packages.NeedModule,
		BuildFlags: []string{"-tags=" + tag},
		Dir:        dir,
	}

	pkgs, err := packages.Load(cfg, pkgPattern)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", pkgPattern, err)
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("expected exactly one package for %s, got %d", pkgPattern, len(pkgs))
	}

	pkg := pkgs[0]

	modulePath := ""
	if pkg.Module != nil {
		modulePath = pkg.Module.Path
	}

	l := &loaded{
		pkg:        pkg,
		modulePath: modulePath,
		pkgName:    pkg.Name,
		// Type errors are collected but not fatal: the container being generated
		// is part of this package, so references to it are legitimately
		// unresolved while bootstrapping or after a rename. Resolution of the
		// provider sets and the inputs declaration below is what actually has to
		// succeed.
		pkgErrors: pkg.Errors,
	}

	// From here the partially-loaded package is returned alongside any error,
	// so the caller can report its type errors as context for the failure.
	sets, markerPath, err := providerSets(pkg, varFilter)
	if err != nil {
		return l, err
	}
	for _, s := range sets {
		ls := &loadedSet{set: s}

		ls.providers = make([]*provider, 0, len(s.elems))
		for i, elem := range s.elems {
			p, err := newProvider(pkg, markerPath, elem, i)
			if err != nil {
				return l, fmt.Errorf("%s: %w", s.varName, err)
			}
			ls.providers = append(ls.providers, p)
		}

		ls.diConfig, ls.inputs, err = containerInputs(pkg, s.containerCtor)
		if err != nil {
			return l, err
		}

		l.sets = append(l.sets, ls)
	}

	return l, nil
}

// providerSets finds every variable of type digen.ProviderSet in the package,
// in source order, and returns the import path of the package declaring that
// type so the markers can be recognised.
func providerSets(pkg *packages.Package, varFilter string) ([]set, string, error) {
	var (
		sets       []set
		markerPath string
		byFile     = map[string]string{}
	)

	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for j, name := range value.Names {
					obj, ok := pkg.TypesInfo.Defs[name].(*types.Var)
					if !ok || obj.Parent() != pkg.Types.Scope() {
						continue
					}
					path := providerSetPkgPath(obj.Type())
					if path == "" {
						continue
					}
					if varFilter != "" && name.Name != varFilter {
						continue
					}
					markerPath = path

					if name.IsExported() {
						return nil, "", fmt.Errorf(
							"provider set %s must be unexported: the generated container type takes "+
								"the exported spelling of its name, which would collide with the variable",
							name.Name,
						)
					}
					if j >= len(value.Values) {
						return nil, "", fmt.Errorf("provider set %s has no value", name.Name)
					}
					lit, ok := value.Values[j].(*ast.CompositeLit)
					if !ok {
						return nil, "", fmt.Errorf("provider set %s must be a composite literal", name.Name)
					}

					srcFile := pkg.Fset.Position(name.Pos()).Filename
					if other, dup := byFile[srcFile]; dup {
						return nil, "", fmt.Errorf(
							"provider sets %s and %s are declared in the same file %s; "+
								"the generated file name comes from the declaring file, so they must be split",
							other, name.Name, filepath.Base(srcFile),
						)
					}
					byFile[srcFile] = name.Name

					containerType := exportName(name.Name)
					sets = append(sets, set{
						varName:       name.Name,
						containerType: containerType,
						containerCtor: "New" + containerType,
						outPath:       strings.TrimSuffix(srcFile, ".go") + generatedSuffix,
						elems:         lit.Elts,
					})
				}
			}
		}
	}

	if len(sets) == 0 {
		if varFilter != "" {
			return nil, "", fmt.Errorf(
				"no %s variable named %s found in %s (is the build tag set?)",
				providerSetType, varFilter, pkg.PkgPath,
			)
		}
		return nil, "", fmt.Errorf(
			"no digen.%s variable found in %s (is the build tag set?)",
			providerSetType, pkg.PkgPath,
		)
	}
	return sets, markerPath, nil
}

// providerSetPkgPath returns the import path of the package declaring t when t
// is a ProviderSet, and "" otherwise. The package is also checked for the two
// markers, so an unrelated type of the same name is not mistaken for one.
func providerSetPkgPath(t types.Type) string {
	obj := typeNameObj(t)
	if obj == nil || obj.Name() != providerSetType || obj.Pkg() == nil {
		return ""
	}
	scope := obj.Pkg().Scope()
	for _, marker := range []string{eagerMarker, asMarker} {
		if _, ok := scope.Lookup(marker).(*types.Func); !ok {
			return ""
		}
	}
	return obj.Pkg().Path()
}

// containerInputs derives the parameters of the generated constructor from its
// own call site, so the signature is stated once, where it is actually used.
//
// The first argument is the container's own configuration; the rest are values
// supplied by the caller and injected into any provider that needs their type.
// This resolves even while bootstrapping, when the constructor itself does not
// exist yet: go/types still records the types of a call's arguments when the
// callee is unresolved.
func containerInputs(pkg *packages.Package, ctorName string) (input, []input, error) {
	calls := findCalls(pkg, ctorName)
	if len(calls) == 0 {
		return input{}, nil, fmt.Errorf(
			"no %s call found in %s; digen derives the container inputs from that call",
			ctorName, pkg.PkgPath,
		)
	}

	args, err := agreeingArgs(pkg, calls, ctorName)
	if err != nil {
		return input{}, nil, err
	}
	if len(args) == 0 {
		return input{}, nil, fmt.Errorf(
			"%s is called with no arguments; the first must be the container configuration",
			ctorName,
		)
	}

	diConfig := input{name: "diCfg", typ: args[0].typ}
	if err := checkContainerConfig(diConfig.typ, ctorName); err != nil {
		return input{}, nil, err
	}

	used := map[string]bool{diConfig.name: true}
	inputs := make([]input, 0, len(args)-1)
	for _, a := range args[1:] {
		name := a.name
		if name == "" {
			name = defaultInputName(a.typ)
		}
		for i := 2; used[name]; i++ {
			name = fmt.Sprintf("%s%d", name, i)
		}
		used[name] = true
		inputs = append(inputs, input{name: name, typ: a.typ})
	}
	return diConfig, inputs, nil
}

// callArg is one argument of the container constructor call: its type, plus the
// identifier it was passed as, which makes the generated parameter names match
// the call site.
type callArg struct {
	name string
	typ  types.Type
}

func findCalls(pkg *packages.Package, name string) []*ast.CallExpr {
	var calls []*ast.CallExpr
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == name {
				calls = append(calls, call)
			}
			return true
		})
	}
	return calls
}

// agreeingArgs reads the arguments of the first call and checks any other calls
// pass the same types, since one signature has to serve them all.
func agreeingArgs(pkg *packages.Package, calls []*ast.CallExpr, ctorName string) ([]callArg, error) {
	args, err := callArgs(pkg, calls[0], ctorName)
	if err != nil {
		return nil, err
	}

	for _, other := range calls[1:] {
		got, err := callArgs(pkg, other, ctorName)
		if err != nil {
			return nil, err
		}
		if len(got) != len(args) {
			return nil, fmt.Errorf(
				"%s is called with %d arguments in one place and %d in another; the calls must agree",
				ctorName, len(args), len(got),
			)
		}
		for i := range args {
			if !types.Identical(args[i].typ, got[i].typ) {
				return nil, fmt.Errorf(
					"%s argument %d is %s in one call and %s in another; the calls must agree",
					ctorName, i+1, typeLabel(args[i].typ), typeLabel(got[i].typ),
				)
			}
		}
	}
	return args, nil
}

func callArgs(pkg *packages.Package, call *ast.CallExpr, ctorName string) ([]callArg, error) {
	out := make([]callArg, 0, len(call.Args))
	for i, arg := range call.Args {
		tv, ok := pkg.TypesInfo.Types[arg]
		if !ok || tv.Type == nil {
			return nil, fmt.Errorf("cannot resolve the type of %s argument %d", ctorName, i+1)
		}
		name := ""
		if ident, ok := arg.(*ast.Ident); ok {
			name = ident.Name
		}
		out = append(out, callArg{name: name, typ: tv.Type})
	}
	return out, nil
}

// defaultInputName derives a parameter name for an argument that is not a plain
// identifier, such as a composite literal.
func defaultInputName(t types.Type) string {
	if obj := typeNameObj(t); obj != nil {
		return unexportName(obj.Name())
	}
	return "input"
}

// checkContainerConfig verifies the container configuration carries the logger
// the generated code writes its debug lines to.
func checkContainerConfig(t types.Type, ctorName string) error {
	strct, ok := t.Underlying().(*types.Struct)
	if !ok {
		return fmt.Errorf(
			"the first %s argument must be a struct carrying the container configuration, got %s",
			ctorName, typeLabel(t),
		)
	}
	for i := range strct.NumFields() {
		field := strct.Field(i)
		if field.Name() != loggerField {
			continue
		}
		if got := types.TypeString(field.Type(), nil); got != loggerType {
			return fmt.Errorf(
				"the container configuration %s has a %s field of type %s, but the generated "+
					"container logs through a %s",
				typeLabel(t), loggerField, got, loggerType,
			)
		}
		return nil
	}
	return fmt.Errorf(
		"the container configuration %s has no %s field, which the generated container logs through",
		typeLabel(t), loggerField,
	)
}

func newProvider(pkg *packages.Package, markerPath string, expr ast.Expr, order int) (*provider, error) {
	expr, eager, alsoAs, err := unwrapMarkers(pkg, markerPath, expr, order)
	if err != nil {
		return nil, err
	}

	var ident *ast.Ident
	switch e := expr.(type) {
	case *ast.Ident:
		ident = e
	case *ast.SelectorExpr:
		ident = e.Sel
	default:
		return nil, fmt.Errorf("provider set entry %d is not a function reference", order)
	}

	obj := pkg.TypesInfo.Uses[ident]
	if obj == nil {
		obj = pkg.TypesInfo.Defs[ident]
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil, fmt.Errorf("provider %q is not a function", ident.Name)
	}

	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("provider %q has no signature", ident.Name)
	}

	results := sig.Results()
	if results.Len() == 0 || results.Len() > 2 {
		return nil, fmt.Errorf("provider %q must return (T) or (T, error), got %d results", ident.Name, results.Len())
	}

	fallible := false
	if results.Len() == 2 {
		if !isErrorType(results.At(1).Type()) {
			return nil, fmt.Errorf("provider %q second result must be error, got %s", ident.Name, results.At(1).Type())
		}
		fallible = true
	}

	params := sig.Params()
	deps := make([]types.Type, 0, params.Len())
	for i := range params.Len() {
		deps = append(deps, params.At(i).Type())
	}

	return &provider{
		fn:       fn,
		provides: results.At(0).Type(),
		alsoAs:   alsoAs,
		deps:     deps,
		variadic: sig.Variadic(),
		fallible: fallible,
		eager:    eager,
		order:    order,
		name:     accessorBaseName(fn),
	}, nil
}

// unwrapMarkers peels digen.Eager and digen.As calls, in either order and
// nested, returning the constructor expression they wrap.
func unwrapMarkers(pkg *packages.Package, markerPath string, expr ast.Expr, order int) (ast.Expr, bool, []types.Type, error) {
	var (
		eager  bool
		alsoAs []types.Type
	)

	for {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return expr, eager, alsoAs, nil
		}

		marker := markerName(pkg, markerPath, call.Fun)
		switch marker {
		case eagerMarker:
			if len(call.Args) != 1 {
				return nil, false, nil, fmt.Errorf("digen.%s takes exactly one provider", eagerMarker)
			}
			eager = true
			expr = call.Args[0]

		case asMarker:
			if len(call.Args) < 2 {
				return nil, false, nil, fmt.Errorf("digen.%s takes a provider and at least one interface", asMarker)
			}
			for _, arg := range call.Args[1:] {
				t, err := interfaceArgType(pkg, arg)
				if err != nil {
					return nil, false, nil, err
				}
				alsoAs = append(alsoAs, t)
			}
			expr = call.Args[0]

		default:
			return nil, false, nil, fmt.Errorf(
				"provider set entry %d is a call to something other than digen.%s or digen.%s",
				order, eagerMarker, asMarker,
			)
		}
	}
}

// markerName returns the digen marker being called, or "" for anything else.
func markerName(pkg *packages.Package, markerPath string, fun ast.Expr) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	fn, ok := pkg.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != markerPath {
		return ""
	}
	return fn.Name()
}

// interfaceArgType resolves a new(Iface) argument to Iface.
func interfaceArgType(pkg *packages.Package, arg ast.Expr) (types.Type, error) {
	tv, ok := pkg.TypesInfo.Types[arg]
	if !ok {
		return nil, fmt.Errorf("cannot resolve the type of a digen.%s argument", asMarker)
	}
	ptr, ok := tv.Type.Underlying().(*types.Pointer)
	if !ok {
		return nil, fmt.Errorf(
			"digen.%s arguments must be written as new(Interface), got %s", asMarker, typeLabel(tv.Type),
		)
	}
	elem := ptr.Elem()
	if !types.IsInterface(elem) {
		return nil, fmt.Errorf("digen.%s argument %s is not an interface", asMarker, typeLabel(elem))
	}
	return elem, nil
}

func isErrorType(t types.Type) bool {
	named, ok := t.(*types.Named)
	return ok && named.Obj().Name() == "error" && named.Obj().Pkg() == nil
}

// accessorBaseName derives an accessor name from the constructor name:
// NewAppService and provideAppService both become AppService. Collisions are
// resolved later by qualifying with the package name.
func accessorBaseName(fn *types.Func) string {
	name := fn.Name()
	for _, prefix := range []string{"New", "provide", "Provide"} {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			name = name[len(prefix):]
			break
		}
	}
	return exportName(name)
}

func exportName(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
