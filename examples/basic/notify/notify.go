// Package notify provides the example's notifiers, its role, and the eager
// registry that collects them.
package notify

import (
	"errors"
	"fmt"

	"github.com/Nethron-Inc/digen/examples/basic/greeting"
	"github.com/Nethron-Inc/digen/examples/basic/store"
)

// Notifier is provided twice, so the container collects both into a Notifiers()
// slice, in provider set order.
type Notifier interface {
	Notify(name string) string
}

// Starter is a role the console notifier is registered under with digen.As,
// even though its constructor returns the narrower Notifier.
type Starter interface {
	Start()
}

type console struct {
	greeter greeting.Greeter
}

// NewConsoleNotifier returns a Notifier that is also a Starter. The proxy the
// container generates implements both, checked by the compiler.
func NewConsoleNotifier(greeter greeting.Greeter) Notifier {
	return &console{greeter: greeter}
}

func (c *console) Notify(name string) string { return "console: " + c.greeter.Greet(name) }

func (c *console) Start() { fmt.Println("console notifier started") }

type audit struct {
	store store.Store
}

// NewAuditNotifier is fallible, which makes the Notifiers() group fallible too:
// it resolves its members in order and forwards the first error.
func NewAuditNotifier(store store.Store) (Notifier, error) {
	if store.Path() == "" {
		return nil, errors.New("audit: store has no path")
	}
	return &audit{store: store}, nil
}

func (a *audit) Notify(name string) string {
	return "audit(" + a.store.Path() + "): " + name
}

// Registry has a side effect in its constructor and nothing resolves it, so the
// provider set marks it eager to have it built at startup anyway.
type Registry struct {
	notifiers []Notifier
}

func NewRegistry(notifiers []Notifier) *Registry {
	fmt.Printf("registry: registered %d notifiers\n", len(notifiers))
	return &Registry{notifiers: notifiers}
}
