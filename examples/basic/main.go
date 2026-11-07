// Command basic demonstrates a digen-generated container.
//
// Run `go run github.com/Nethron-Inc/digen/cmd/digen -pkg ./examples/basic` from the module
// root to regenerate providers_gen.go.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/Nethron-Inc/digen"
	"github.com/Nethron-Inc/digen/examples/basic/config"
	"github.com/Nethron-Inc/digen/examples/basic/greeting"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := config.Config{Greeting: "Hello", DataDir: "/tmp/digen-example"}

	// The arguments here are what give the generated constructor its signature:
	// the container configuration first, then every value injected into it.
	c, err := NewAppContainer(digen.ContainerConfig{Logger: logger}, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "building the container:", err)
		os.Exit(1)
	}

	// The console notifier fills the Starter role, and reaching it through the
	// proxy builds it here rather than when the container was created.
	c.Starter().Start()

	// Notifiers() is fallible because the audit notifier is, so the first
	// failure comes back here rather than panicking somewhere below.
	notifiers, err := c.Notifiers()
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolving notifiers:", err)
		os.Exit(1)
	}
	for _, n := range notifiers {
		fmt.Println(n.Notify("world"))
	}

	// The test container has the same providers, but its Greeter is the one
	// passed in, so the console notifier it builds greets with the stub.
	tc, err := newTestContainer(cfg, stubGreeter{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "building the test container:", err)
		os.Exit(1)
	}
	testNotifiers, err := tc.Notifiers()
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolving test notifiers:", err)
		os.Exit(1)
	}
	fmt.Println(testNotifiers[0].Notify("world"))
}

// newTestContainer is the call site that gives NewTestContainer its signature:
// the configuration, then the Greeter that replaces the real one. A test would
// pass a mock here.
func newTestContainer(cfg config.Config, greeter greeting.Greeter) (*TestContainer, error) {
	return NewTestContainer(digen.ContainerConfig{}, cfg, greeter)
}

// stubGreeter stands in for a test double.
type stubGreeter struct{}

func (stubGreeter) Greet(name string) string { return "stub greets " + name }
