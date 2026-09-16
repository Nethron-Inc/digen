package generator

import (
	"fmt"
	"go/types"
	"sort"
	"strings"
)

// binding records what satisfies one dependency of one provider: either an
// injected input, or one or more providers.
type binding struct {
	dep     types.Type
	slice   bool // the dependency is []E, so every match is collected
	targets []*provider
	input   *input // set instead of targets when an injected value satisfies it
}

// node is a provider plus its resolved dependencies.
type node struct {
	p        *provider
	bindings []binding
	proxied  bool
	// fallible is whether the accessor returns an error, which is true when the
	// provider itself can fail or any dependency it resolves can.
	fallible bool
	// needsMust is whether an infallible builder consumes this, so a panicking
	// accessor has to be emitted alongside the error-returning one.
	needsMust bool
}

type graph struct {
	modulePath string
	targetPath string // package the container is generated into
	pkgName    string // its package clause
	outPath    string // the file the container is written to
	// containerType names the generated type and containerCtor its constructor.
	// Both are derived from the provider set variable's name.
	containerType string
	containerCtor string
	// prefixProxies qualifies generated proxy type names with the container
	// name. Proxy types are package-level, so two containers generated into the
	// same package would otherwise collide.
	prefixProxies bool
	providers     []*provider
	nodes         []*node
	byProvider    map[*provider]*node
	diConfig      input
	inputs        []input
	// groups are types satisfied by several providers, or consumed as []T;
	// each gets a slice accessor. Keyed by the accessor name.
	groups []group
	// roles are digen.As types filled by exactly one provider, which additionally
	// get a singular accessor.
	roles []role
}

// group is a type with a slice accessor collecting every provider of it.
type group struct {
	name      string
	elem      types.Type
	targets   []*provider
	fallible  bool
	needsMust bool
}

// role is a type declared with digen.As that exactly one provider fills, so it can
// also be reached as a single value. Once a second provider fills it the role
// disappears and only the group accessor remains.
type role struct {
	name   string
	typ    types.Type
	target *provider
}

func (g *graph) proxyCount() int {
	n := 0
	for _, nd := range g.nodes {
		if nd.proxied {
			n++
		}
	}
	return n
}

func resolve(l *loaded, s *loadedSet) (*graph, error) {
	g := &graph{
		modulePath:    l.modulePath,
		targetPath:    l.pkg.PkgPath,
		pkgName:       l.pkgName,
		outPath:       s.outPath,
		containerType: s.containerType,
		containerCtor: s.containerCtor,
		prefixProxies: len(l.sets) > 1,
		providers:     s.providers,
		byProvider:    make(map[*provider]*node, len(s.providers)),
		diConfig:      s.diConfig,
		inputs:        s.inputs,
	}

	if err := assignNames(s.providers); err != nil {
		return nil, err
	}

	grouped := map[string]*group{}

	for _, p := range s.providers {
		if p.variadic {
			return nil, fmt.Errorf("provider %s is variadic, which digen cannot wire", p)
		}

		nd := &node{p: p, proxied: shouldProxy(p, g.modulePath)}
		for _, dep := range p.deps {
			b, err := bindDep(s.providers, s.inputs, p, dep)
			if err != nil {
				return nil, err
			}
			if b.slice {
				g.addGroup(grouped, b.dep, b.targets)
			}
			nd.bindings = append(nd.bindings, b)
		}

		g.nodes = append(g.nodes, nd)
		g.byProvider[p] = nd
	}

	// Any type several providers satisfy gets a slice accessor too, even when
	// nothing consumes it as a slice yet.
	for _, t := range providedTypes(s.providers) {
		if matches := candidates(s.providers, t, false); len(matches) > 1 {
			g.addGroup(grouped, t, matches)
		}
	}

	// A type named in digen.As is a role the component was deliberately registered
	// under, so it always gets a slice accessor however few providers fill it.
	// Code written against the plural then survives a second one appearing.
	for _, t := range taggedTypes(s.providers) {
		g.addGroup(grouped, t, candidates(s.providers, t, false))
	}
	sort.Slice(g.groups, func(i, j int) bool { return g.groups[i].name < g.groups[j].name })

	g.addRoleAccessors(s.providers)

	if err := g.checkCycles(); err != nil {
		return nil, err
	}

	g.computeFallibility()
	return g, nil
}

// computeFallibility marks which accessors return an error.
//
// An accessor is fallible when its provider can fail or when any dependency it
// resolves is itself fallible. A proxied accessor is never fallible: it hands
// back the proxy without building anything, so contagion stops there — which is
// why fallible accessors also recover, to catch a failure that happens later
// underneath a proxy.
//
// The graph is known to be acyclic by this point, so a memoised DFS terminates.
func (g *graph) computeFallibility() {
	state := make(map[*node]int, len(g.nodes)) // 0 unvisited, 1 in progress, 2 done

	var visit func(nd *node) bool
	visit = func(nd *node) bool {
		switch state[nd] {
		case 1, 2:
			return nd.fallible
		}
		state[nd] = 1

		fallible := nd.p.fallible
		if !nd.proxied {
			for _, b := range nd.bindings {
				if b.input != nil {
					continue
				}
				for _, target := range b.targets {
					if visit(g.byProvider[target]) {
						fallible = true
					}
				}
			}
		}

		nd.fallible = fallible
		state[nd] = 2
		return fallible
	}

	for _, nd := range g.nodes {
		visit(nd)
	}

	// A proxy hands back the proxy value, so its accessor cannot fail even
	// though its build closure reaches fallible dependencies later.
	for _, nd := range g.nodes {
		if nd.proxied {
			nd.fallible = false
		}
	}

	for i := range g.groups {
		for _, t := range g.groups[i].targets {
			if g.byProvider[t].fallible {
				g.groups[i].fallible = true
			}
		}
	}

	g.markMustNeeded()
}

// markMustNeeded finds fallible accessors consumed by a builder that has no
// error return, which is where a panicking must accessor is still required.
func (g *graph) markMustNeeded() {
	for _, nd := range g.nodes {
		// A proxy's build closure returns only a value, so it always needs must.
		if nd.fallible {
			continue
		}
		for _, b := range nd.bindings {
			if b.input != nil {
				continue
			}
			if b.slice {
				for i := range g.groups {
					if types.Identical(g.groups[i].elem, b.dep) && g.groups[i].fallible {
						g.groups[i].needsMust = true
					}
				}
				continue
			}
			for _, target := range b.targets {
				if dep := g.byProvider[target]; dep.fallible {
					dep.needsMust = true
				}
			}
		}
	}
}

// addGroup records a slice accessor for elem, deduplicating by element type.
func (g *graph) addGroup(seen map[string]*group, elem types.Type, targets []*provider) {
	key := types.TypeString(elem, nil)
	if _, ok := seen[key]; ok {
		return
	}
	gr := group{name: groupName(elem, targets), elem: elem, targets: targets}
	seen[key] = &gr
	g.groups = append(g.groups, gr)
}

// groupName derives the slice accessor name from the element type.
func groupName(elem types.Type, targets []*provider) string {
	base := ""
	if obj := typeNameObj(elem); obj != nil {
		base = obj.Name()
	} else if len(targets) > 0 {
		base = targets[0].name
	}
	return exportName(base) + "s"
}

// addRoleAccessors records a singular accessor for every digen.As type filled by
// exactly one provider. The name is the interface's own, qualified with its
// package if that is taken, and dropped if both are — the group accessor covers
// the type either way.
func (g *graph) addRoleAccessors(providers []*provider) {
	taken := map[string]bool{}
	for _, p := range providers {
		taken[p.name] = true
	}
	for _, gr := range g.groups {
		taken[gr.name] = true
	}

	for _, t := range taggedTypes(providers) {
		matches := candidates(providers, t, false)
		if len(matches) != 1 {
			continue
		}

		obj := typeNameObj(t)
		if obj == nil {
			continue
		}

		name := exportName(obj.Name())
		if taken[name] && obj.Pkg() != nil {
			name = exportName(obj.Pkg().Name()) + exportName(obj.Name())
		}
		if taken[name] {
			continue
		}

		taken[name] = true
		g.roles = append(g.roles, role{name: name, typ: t, target: matches[0]})
	}

	sort.Slice(g.roles, func(i, j int) bool { return g.roles[i].name < g.roles[j].name })
}

// taggedTypes lists the distinct interfaces named in digen.As calls, in provider
// set order.
func taggedTypes(providers []*provider) []types.Type {
	var out []types.Type
	seen := map[string]bool{}
	for _, p := range providers {
		for _, t := range p.alsoAs {
			key := types.TypeString(t, nil)
			if !seen[key] {
				seen[key] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// providedTypes lists every distinct type the providers offer, including the
// extra interfaces declared with digen.As.
func providedTypes(providers []*provider) []types.Type {
	var out []types.Type
	seen := map[string]bool{}
	add := func(t types.Type) {
		key := types.TypeString(t, nil)
		if !seen[key] {
			seen[key] = true
			out = append(out, t)
		}
	}
	for _, p := range providers {
		add(p.provides)
		for _, t := range p.alsoAs {
			add(t)
		}
	}
	return out
}

// bindDep resolves one dependency to the provider(s) that satisfy it.
//
// An exact type match wins outright. Otherwise any provider whose provided type
// implements the dependency's interface qualifies — that is what collects the
// four RESTControllers into []RESTController and lets one WebsocketModule
// satisfy both HTTPServerModule and WebsocketMessageBroker.
func bindDep(all []*provider, inputs []input, dependent *provider, dep types.Type) (binding, error) {
	// An injected value wins outright: it is supplied, not built.
	for i := range inputs {
		if types.Identical(inputs[i].typ, dep) {
			return binding{dep: dep, input: &inputs[i]}, nil
		}
	}

	if slice, ok := dep.Underlying().(*types.Slice); ok {
		elem := slice.Elem()
		matches := candidates(all, elem, false)
		if len(matches) == 0 {
			return binding{}, fmt.Errorf(
				"no provider for %s, required as %s by %s",
				typeLabel(elem), typeLabel(dep), dependent,
			)
		}
		return binding{dep: elem, slice: true, targets: matches}, nil
	}

	if exact := candidates(all, dep, true); len(exact) == 1 {
		return binding{dep: dep, targets: exact}, nil
	} else if len(exact) > 1 {
		return binding{}, ambiguous(dependent, dep, exact)
	}

	matches := candidates(all, dep, false)
	switch len(matches) {
	case 1:
		return binding{dep: dep, targets: matches}, nil
	case 0:
		return binding{}, fmt.Errorf("no provider for %s, required by %s", typeLabel(dep), dependent)
	default:
		return binding{}, ambiguous(dependent, dep, matches)
	}
}

func ambiguous(dependent *provider, dep types.Type, matches []*provider) error {
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m.String())
	}
	return fmt.Errorf(
		"%s is required by %s but provided by %d providers: %s\n"+
			"\thint: declare the dependency as []%s to collect them all, or narrow the provided types",
		typeLabel(dep), dependent, len(matches), strings.Join(names, ", "), typeLabel(dep),
	)
}

// candidates returns providers satisfying want, in provider set order. A
// provider qualifies by its declared result, by an interface declared with
// digen.As, or by implementing want.
func candidates(all []*provider, want types.Type, exactOnly bool) []*provider {
	var out []*provider
	for _, p := range all {
		if types.Identical(p.provides, want) || identicalToAny(p.alsoAs, want) {
			out = append(out, p)
			continue
		}
		if exactOnly {
			continue
		}
		if iface, ok := want.Underlying().(*types.Interface); ok && iface.NumMethods() > 0 {
			if types.Implements(p.provides, iface) || implementsAny(p.alsoAs, iface) {
				out = append(out, p)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].order < out[j].order })
	return out
}

func identicalToAny(ts []types.Type, want types.Type) bool {
	for _, t := range ts {
		if types.Identical(t, want) {
			return true
		}
	}
	return false
}

func implementsAny(ts []types.Type, iface *types.Interface) bool {
	for _, t := range ts {
		if types.Implements(t, iface) {
			return true
		}
	}
	return false
}

// checkCycles reports the first dependency cycle found, as a readable path.
func (g *graph) checkCycles() error {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	state := make(map[*provider]int, len(g.providers))
	var stack []*provider

	var visit func(p *provider) error
	visit = func(p *provider) error {
		switch state[p] {
		case grey:
			at := 0
			for i, s := range stack {
				if s == p {
					at = i
					break
				}
			}
			path := make([]string, 0, len(stack)-at+1)
			for _, s := range stack[at:] {
				path = append(path, s.String())
			}
			path = append(path, p.String())
			return fmt.Errorf("dependency cycle: %s", strings.Join(path, " -> "))
		case black:
			return nil
		}

		state[p] = grey
		stack = append(stack, p)
		for _, b := range g.byProvider[p].bindings {
			if b.input != nil {
				continue
			}
			for _, target := range b.targets {
				if err := visit(target); err != nil {
					return err
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[p] = black
		return nil
	}

	for _, p := range g.providers {
		if err := visit(p); err != nil {
			return err
		}
	}
	return nil
}

// assignNames gives every provider a unique accessor name, qualifying with the
// package name when constructors in different packages share a name — as the
// four NewRESTController constructors do.
func assignNames(providers []*provider) error {
	byName := map[string][]*provider{}
	for _, p := range providers {
		byName[p.name] = append(byName[p.name], p)
	}

	for name, group := range byName {
		if len(group) == 1 {
			continue
		}
		for _, p := range group {
			pkgName := ""
			if p.fn.Pkg() != nil {
				pkgName = p.fn.Pkg().Name()
			}
			p.name = exportName(pkgName) + name
		}
	}

	seen := map[string]*provider{}
	for _, p := range providers {
		if other, dup := seen[p.name]; dup {
			return fmt.Errorf("providers %s and %s both map to accessor %s", other, p, p.name)
		}
		seen[p.name] = p
	}
	return nil
}

// shouldProxy reports whether a provider's result gets a lazy proxy.
//
// Only interfaces declared inside this module qualify. Structs cannot be
// proxied at all, third-party and stdlib interfaces are left alone (proxying
// context.Context would break its semantics), fallible providers are excluded
// so their error surfaces at the accessor rather than on a later method call,
// and eager providers need no proxy because they are built immediately.
func shouldProxy(p *provider, modulePath string) bool {
	if p.fallible || p.eager || modulePath == "" {
		return false
	}
	iface, ok := p.provides.Underlying().(*types.Interface)
	if !ok || iface.NumMethods() == 0 {
		return false
	}
	obj := typeNameObj(p.provides)
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return strings.HasPrefix(obj.Pkg().Path(), modulePath)
}

func typeNameObj(t types.Type) *types.TypeName {
	switch n := t.(type) {
	case *types.Named:
		return n.Obj()
	case *types.Alias:
		return n.Obj()
	}
	return nil
}

// typeLabel renders a type for diagnostics, keeping the package short.
func typeLabel(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Name() })
}
