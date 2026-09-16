// Command digen generates a dependency injection container from a provider set.
//
// It reads every digen.ProviderSet variable of the target package (built with
// the digen build tag) and writes one container per set, next to the file
// declaring it: providers.go yields providers_gen.go, and a set named
// appContainer yields the type AppContainer and the constructor NewAppContainer.
//
// Wiring mistakes fail here rather than at startup: a dependency with no
// provider, a dependency satisfied by more than one provider, and dependency
// cycles are all reported as errors.
//
// Usage:
//
//	digen [-dir .] [-pkg ./cmd/app] [-tag digen] [-var appContainer]
package main

import (
	"flag"
	"fmt"
	"os"

	"bugdrill.ai/digen/internal/generator"
)

func main() {
	opts := generator.Options{}
	flag.StringVar(&opts.Dir, "dir", ".", "directory of the module to load")
	flag.StringVar(&opts.Pkg, "pkg", ".", "package containing the provider set")
	flag.StringVar(&opts.Tag, "tag", "digen", "build tag the provider set is written behind")
	flag.StringVar(&opts.Var, "var", "", "generate only this provider set (default: every set in the package)")
	flag.Parse()

	if err := generator.Generate(opts); err != nil {
		fmt.Fprintf(os.Stderr, "digen: %v\n", err)
		os.Exit(1)
	}
}
