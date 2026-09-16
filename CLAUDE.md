# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

The project uses [`just`](https://github.com/casey/just) (see `justfile`):

```bash
just install           # go install ./cmd/digen into GOBIN
just vet               # go vet ./...
just generate-example  # go run ./cmd/digen -pkg ./examples/basic
just run-example       # regenerate, then go run ./examples/basic
just tidy              # go mod tidy && go fmt ./...
just upgrade           # interactive dependency upgrade
```

There are no tests in this repository. `examples/basic` is the working verification
harness: regenerate it and inspect `git diff examples/basic/providers_gen.go` to see
exactly what a change does to the emitted code, then `just run-example` to confirm the
result compiles and behaves. Any change to `internal/generator` should be accompanied by a
regenerated `providers_gen.go` in the same commit.

## Architecture

digen is a build-time DI container generator: it reads a `digen.ProviderSet` variable out
of a package's syntax tree and writes a container next to it. The generated code is plain
Go with no runtime container and no reflection.

**Two packages face outward, one does the work.**

- `digen.go` (package `digen`) — the markers a consumer imports: `ProviderSet`, `Eager`,
  `As`, `ContainerConfig`. All the marker functions are runtime no-ops; they exist so
  intent is expressed in compiler-checked Go and read back from the AST by the generator.
  **This package must never import anything outside the standard library** — that is what
  lets a service depend on the markers without pulling `golang.org/x/tools` into its build
  graph. The command is installed separately for the same reason.
- `cmd/digen/main.go` — flag parsing only (`-dir`, `-pkg`, `-tag`, `-var`).
- `internal/generator` — the pipeline, in three stages that run in this order.

**The pipeline: `load.go` → `graph.go` → `render.go`, orchestrated by `generator.go`.**

`load.go` loads the target package with `packages.Load` under the `-tag` build tag (default
`digen`), finds every `ProviderSet` variable, peels `Eager`/`As` markers off each element
(they nest in either order), and produces `[]*provider` — constructor, provided type,
dependencies, fallible/eager/variadic flags, and declaration order.

Two derivations here are unusual and worth knowing:

- **Everything is named by declaration, nothing is configured.** `var appContainer` in
  `providers.go` yields type `AppContainer`, constructor `NewAppContainer`, written to
  `providers_gen.go`. The set variable must be unexported because the container takes the
  exported spelling. There is deliberately no output flag.
- **The container constructor's signature is read off its own call site** (`containerInputs`).
  digen finds calls to `NewAppContainer(...)`, takes the first argument as the container
  configuration (any struct with a `Logger *slog.Logger` field) and the rest as *injected
  values* available to any provider taking those types. `go/types` records argument types
  even when the callee is unresolved, so this works while bootstrapping, before the
  container exists.

`graph.go` resolves each dependency to a binding (`bindDep`): an injected input wins
outright, then an exact type match, then any provider whose result implements the
dependency's interface. It then computes the derived properties that shape the output:

- **Groups** — a type provided more than once, consumed as `[]T`, or named in `digen.As`
  gets a slice accessor in provider-set order. **Roles** — a `digen.As` type filled by
  exactly one provider also gets a singular accessor, which disappears if a second provider
  appears (so callers written against the plural survive that).
- **Fallibility is contagious, and proxies stop it.** An accessor returns `(T, error)` if
  its provider or anything it resolves can fail. A proxied accessor never fails — it hands
  back the proxy without building — which is why fallible accessors also `recover`: a
  failure raised later under a proxy boundary comes back as an error rather than a panic.
- **`shouldProxy`** admits only interfaces declared inside the target module. Structs,
  third-party and stdlib interfaces (proxying `context.Context` would break its semantics),
  fallible providers, and eager providers are never proxied.
- `needsMust` marks fallible accessors consumed by an infallible builder, which is where
  the panicking `mustX()` variant is emitted alongside the error-returning one.
- Cycles, unprovided dependencies, ambiguous dependencies and variadic constructors are all
  rejected here, named after the provider that caused them.

`render.go` writes proxies, the container struct and constructor, accessors, roles and
groups, in that order. **Package references are emitted as `\x00pkg:path\x00` tokens and
replaced with import aliases only at the end**, so alias assignment is deterministic
(sorted by import path) rather than dependent on the order types happened to be written.
Never write a raw import alias while rendering — go through `typeString`/`pkgToken`. The
whole buffer is `format.Source`d; a failure dumps numbered source, which is the main
debugging aid when the emitted code is invalid.

**Two ordering invariants in `generator.go`:**

- Every container is rendered before any file is written, so a failure in the second set
  does not leave the first one's file half-updated.
- Type errors in the target package are collected, not reported eagerly. The container is
  *part of* the package being generated, so a stale or missing generated file makes that
  package fail to typecheck — the exact state a run exists to fix. They are printed only as
  context for a failure, or, after a successful run, as the errors that survived
  regeneration (`remainingErrors` reloads the package to find out).

## Conventions

Comments in this codebase explain *why* a decision was made, often at length, and the
package doc comments carry real design rationale. Match that register rather than
annotating what the code plainly does. The README is written the same way and is the
user-facing spec — behaviour changes belong in it.
