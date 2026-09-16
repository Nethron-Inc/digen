// Command basic demonstrates a digen-generated container.
//
// Run `go run bugdrill.ai/digen/cmd/digen -pkg ./examples/basic` from the module
// root to regenerate providers_gen.go.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"bugdrill.ai/digen"
	"bugdrill.ai/digen/examples/basic/config"
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
}
