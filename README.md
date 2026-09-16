# digen

Build-time dependency injection for Go. You declare a provider set; digen reads it and
writes the container — one accessor per component, lazy proxies for interfaces, and wiring
mistakes reported at generation time rather than at startup.

No runtime container, no reflection, no struct tags. The generated code is ordinary Go you
can read, and the package you import to declare a set depends on nothing outside the
standard library.

## Install

```bash
go install bugdrill.ai/digen/cmd/digen@latest
```

`bugdrill.ai` is not a real host, so a consumer resolves the module through a replace
directive:

```
require bugdrill.ai/digen v0.0.0

replace bugdrill.ai/digen => ../digen
```

Installing the command separately is deliberate: it keeps `golang.org/x/tools`, which the
generator needs, out of your service's own build graph. Importing `bugdrill.ai/digen` for
the markers costs you nothing.

## Declare a provider set

```go
//go:build digen

package main

import (
	"bugdrill.ai/digen"
	"example.com/app/greeting"
	"example.com/app/notify"
	"example.com/app/store"
)

var appContainer = digen.ProviderSet{
	greeting.NewGreeter,
	store.NewStore,

	// Registered under a role wider than the type it returns.
	digen.As(notify.NewConsoleNotifier, new(notify.Starter)),
	notify.NewAuditNotifier,

	// Nothing resolves the registry, so build it at startup anyway.
	digen.Eager(notify.NewRegistry),
}
```

Each entry is a constructor: its parameters are its dependencies, its first result is what
it provides, and an optional second result is `error`. The file carries a build tag so it is
excluded from normal builds — it exists to be read by the generator.

Then:

```bash
digen -pkg ./cmd/app
```

## Names come from the declaration

Nothing is configured. The variable and the file it lives in decide everything:

| From | To |
|---|---|
| `var appContainer` | type `AppContainer`, constructor `NewAppContainer` |
| `providers.go` | `providers_gen.go`, written next to it |

The set variable must be unexported, since the container takes the exported spelling of its
name. A package may hold several sets, one per file; each gets its own container, and their
proxy types are qualified so they do not collide.

## The constructor's signature comes from its call site

digen does not ask you to describe the container's inputs. It reads the call:

```go
c, err := NewAppContainer(digen.ContainerConfig{Logger: logger}, ctx, cfg)
```

The first argument is the container's own configuration. Everything after it is an *injected
value*: supplied by you rather than built, and available to any provider that takes that
type. Here every provider taking a `config.Config` gets `cfg`, and one taking a
`context.Context` gets `ctx`.

This resolves even while bootstrapping, when `NewAppContainer` does not exist yet, so the
first generation works from the call you have not been able to compile.

The configuration does not have to be `digen.ContainerConfig`; any struct with a
`Logger *slog.Logger` field will do. A nil logger is fine — the container discards its
instantiation trace rather than failing. Set one and every component logs as it is built:

```
level=DEBUG msg=Instantiating component=AppContainer target.component=Greeter
```

## What you get

```go
c.Greeter()                   // the same instance every time
c.Store()                     // (Store, error) — its provider can fail
c.Notifiers()                 // every Notifier, in provider set order
c.Starter()                   // the single provider registered under that role
```

**Lazy proxies.** An accessor for an interface declared inside your module returns a proxy:
a value implementing the interface that builds the real component on its first method call.
Passing a dependency to a constructor therefore does not construct it, and a deep graph
costs nothing until it is used. Structs, third-party interfaces, and fallible and eager
providers are not proxied.

**Errors propagate.** An accessor returns `(T, error)` when its provider or anything it
resolves can fail, forwarding the first error unchanged. Fallible accessors also recover, so
a failure raised below a proxy boundary comes back as an error rather than a panic.

**Order is preserved.** A type provided several times is collected into a slice in provider
set declaration order — which is how a set pins the order routes or services are registered
in.

**Roles.** `digen.As` registers a provider under interfaces beyond its declared result. Every
tagged type gets a slice accessor (`Observers()`); one filled by exactly one provider also
gets a singular one (`Observer()`), so code written against the plural survives a second
implementation appearing.

## Failures happen at generation time

A dependency with no provider, one satisfied by more than one provider, a cycle, a variadic
constructor, a missing constructor call site, a container configuration without a usable
`Logger` — all of these fail the generate step, with the provider named:

```
digen: campaign.PipelineRepository is required by campaign.NewCampaignService but provided by 2 providers: ...
	hint: declare the dependency as []campaign.PipelineRepository to collect them all, or narrow the provided types
```

Type errors in the target package are reported with more care. The container is *part of*
the package being generated, so a stale or missing generated file makes that package fail to
typecheck — which is the state a generation run exists to fix. digen therefore stays quiet
about them until they still matter: as context for a failure, or, after a successful run, as
the errors that survived regeneration. A clean run prints one line per file written.

## Options

```
digen [-dir .] [-pkg .] [-tag digen] [-var name]
```

| Flag | Meaning |
|---|---|
| `-dir` | Directory of the module to load. Default `.` |
| `-pkg` | Package pattern holding the provider set(s). Default `.` |
| `-tag` | Build tag the set is written behind. Default `digen` |
| `-var` | Generate only this set, rather than every set in the package |

There is no output flag: the path is always derived from the file declaring the set.

## Example

`examples/basic` is a complete working container — lazy proxies, a fallible provider, a
role, a collected slice and an eager component with a startup side effect:

```bash
go run ./cmd/digen -pkg ./examples/basic   # regenerate
go run ./examples/basic                    # run it
```
