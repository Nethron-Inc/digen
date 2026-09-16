//go:build digen

package main

import (
	"github.com/Nethron-Inc/digen"
	"github.com/Nethron-Inc/digen/examples/basic/greeting"
	"github.com/Nethron-Inc/digen/examples/basic/notify"
	"github.com/Nethron-Inc/digen/examples/basic/store"
)

// appContainer is the single source of truth for how the example is wired.
//
// Each entry is a constructor: its parameters are its dependencies and its
// first result is what it provides. digen reads this list and generates
// providers_gen.go next to it, holding AppContainer and NewAppContainer — the
// names come from this variable's own.
//
// Declaration order is preserved for types provided more than once: the two
// Notifiers are collected into a slice in the order they appear here.
//
// This file is excluded from normal builds by the digen tag; it exists only to
// be read by the generator.
var appContainer = digen.ProviderSet{
	greeting.NewGreeter,
	store.NewStore,

	// Also a Starter, a role wider than the Notifier the constructor returns.
	digen.As(notify.NewConsoleNotifier, new(notify.Starter)),
	notify.NewAuditNotifier,

	// Nothing resolves the registry, so it is built at startup instead.
	digen.Eager(notify.NewRegistry),
}
