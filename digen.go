// Package digen holds the markers and configuration shared by a provider set
// and the container generated from it by the digen command.
//
// The markers are no-ops at runtime: they exist so the provider set can express
// intent in ordinary, compiler-checked Go rather than in comments. The generator
// reads them from the syntax tree.
//
// This package imports nothing outside the standard library, so depending on it
// costs a project nothing at build time.
package digen

import "log/slog"

// ProviderSet is the declarative list of constructors a container is generated
// from. Each entry is a function whose parameters are its dependencies and whose
// first result is what it provides, optionally wrapped in [Eager] or [As].
//
// Declaring a variable of this type is what marks it for the generator: the
// variable's name gives the container its name, and the file it is declared in
// gives the generated file its name.
//
//	var appContainer = digen.ProviderSet{
//		app.NewAppRepository,
//		digen.Eager(discovery.NewExplorer),
//	}
//
// generates AppContainer and NewAppContainer into providers_gen.go next to it.
// The variable must be unexported, since the generated type takes the exported
// spelling of its name.
type ProviderSet []any

// Eager marks a provider whose component is constructed when the container is
// created rather than on first use. Use it for constructors with a side effect
// that has to happen at startup — starting a goroutine, registering a job —
// which would otherwise never run if nothing resolves them.
//
//	digen.Eager(campaign.NewScheduler)
func Eager(fn any) any { return fn }

// As registers a provider under additional interfaces beyond its declared
// result type, for constructors returning a type narrower than the roles the
// value actually fills. Pass each interface as new(Iface).
//
//	digen.As(api.NewWebsocketModule, new(api.WebsocketMessageBroker))
//
// Eager and As nest in either order.
func As(fn any, ifaces ...any) any { return fn }

// Include adds every provider of another set declared in the same package, at
// the position of the call, as if they had been written there. It is how one
// wiring serves several containers: a test container includes the production
// set and differs from it only in what its constructor is passed.
//
//	var testContainer = digen.ProviderSet{
//		digen.Include(container),
//	}
//
// A value passed to a container's constructor always wins over a provider of
// the same type, so calling NewTestContainer with a stub of some interface
// replaces that dependency for everything the test container builds, while the
// production container, called without it, keeps the real one.
//
// Include takes the set variable by name and may only appear directly in a
// set, not inside Eager or As. Includes nest; a cycle is a generation error.
func Include(set ProviderSet) any { return set }

// ContainerConfig configures a generated container itself, as opposed to the
// components it builds.
//
// A container's constructor takes its configuration as the first argument. That
// argument does not have to be this type — any struct with a Logger field of
// type *slog.Logger will do — but this is the one to reach for when there is
// nothing project-specific to add.
type ContainerConfig struct {
	// Logger receives a debug line for every component the container
	// instantiates. It is not passed to the components themselves, and a nil
	// Logger is fine: the container then discards those lines.
	Logger *slog.Logger
}
