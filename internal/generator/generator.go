// Package generator reads a digen.ProviderSet and writes the container
// generated from it.
//
// It resolves every constructor's parameters to other providers by type and
// writes a container with one accessor per provider. Wiring mistakes fail here
// rather than at startup: a dependency with no provider, a dependency satisfied
// by more than one provider, and dependency cycles are all reported as errors.
//
// Interfaces declared inside the target module get a lazy proxy — a value
// implementing the interface that builds the real one on first method call — so
// injecting a dependency does not construct it. See render.go for the generated
// shapes.
package generator

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/tools/go/packages"
)

// Options configures one generation run.
type Options struct {
	// Dir is the directory to load the target module from.
	Dir string
	// Pkg is the package pattern holding the provider set(s).
	Pkg string
	// Tag is the build tag the provider set file is written behind.
	Tag string
	// Var optionally narrows generation to a single provider set variable.
	Var string
	// Out receives the progress and diagnostic lines. Defaults to os.Stderr.
	Out io.Writer
}

func (o Options) out() io.Writer {
	if o.Out == nil {
		return os.Stderr
	}
	return o.Out
}

// Generate writes one container per provider set found in the target package.
//
// Type errors in that package are handled with some care. The container is part
// of the package being generated, so a stale — or missing — generated file makes
// the package fail to typecheck, and that is precisely the state a generation
// run exists to fix. Reporting those errors up front means a successful run
// buries its own output in diagnostics that the run itself resolved. So they are
// collected, and reported only when they still matter: as context for a failure,
// or, after a successful run, as the errors that survived regeneration.
func Generate(opts Options) error {
	loaded, err := load(opts.Dir, opts.Pkg, opts.Tag, opts.Var)
	if err != nil {
		if loaded != nil {
			reportErrors(opts.out(), loaded.pkgErrors, "package has type errors that may explain the failure")
		}
		return err
	}

	type output struct {
		path      string
		source    []byte
		providers int
		proxies   int
	}

	outputs := make([]output, 0, len(loaded.sets))
	for _, s := range loaded.sets {
		graph, err := resolve(loaded, s)
		if err != nil {
			reportErrors(opts.out(), loaded.pkgErrors, "package has type errors that may explain the failure")
			return err
		}

		source, err := render(graph)
		if err != nil {
			reportErrors(opts.out(), loaded.pkgErrors, "package has type errors that may explain the failure")
			return err
		}

		outputs = append(outputs, output{
			path:      graph.outPath,
			source:    source,
			providers: len(graph.providers),
			proxies:   graph.proxyCount(),
		})
	}

	// Nothing is written until every container renders, so a failure in the
	// second set does not leave the first one's file half-updated on disk.
	for _, o := range outputs {
		if err := os.WriteFile(o.path, o.source, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", o.path, err)
		}
		fmt.Fprintf(opts.out(), "digen: wrote %s (%d providers, %d proxies)\n",
			relative(opts.Dir, o.path), o.providers, o.proxies)
	}

	reportErrors(opts.out(), remainingErrors(opts), "package still has type errors after generation")
	return nil
}

// remainingErrors reloads the target package now that the containers are on
// disk, so only the errors the regeneration did not resolve are left. A failure
// to reload is not itself reported: the run succeeded, and this is a diagnostic
// courtesy rather than part of the result.
func remainingErrors(opts Options) []packages.Error {
	cfg := &packages.Config{
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports,
		BuildFlags: []string{"-tags=" + opts.Tag},
		Dir:        opts.Dir,
	}
	pkgs, err := packages.Load(cfg, opts.Pkg)
	if err != nil || len(pkgs) != 1 {
		return nil
	}
	return pkgs[0].Errors
}

func reportErrors(out io.Writer, errs []packages.Error, headline string) {
	if len(errs) == 0 {
		return
	}
	fmt.Fprintf(out, "digen: %s:\n", headline)
	for _, e := range errs {
		fmt.Fprintf(out, "digen:   %v\n", e)
	}
}

// relative shortens a path for reporting, falling back to the path itself.
func relative(dir, path string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(abs, path)
	if err != nil {
		return path
	}
	return rel
}
