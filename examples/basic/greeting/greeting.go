// Package greeting provides the example's greeter.
package greeting

import "bugdrill.ai/digen/examples/basic/config"

// Greeter is an interface declared inside this module, so the container hands
// out a lazy proxy for it: taking a Greeter as a dependency does not build one.
type Greeter interface {
	Greet(name string) string
}

type greeter struct {
	prefix string
}

// NewGreeter is infallible and returns a module-owned interface, which is
// exactly what the container proxies.
func NewGreeter(cfg config.Config) Greeter {
	return &greeter{prefix: cfg.Greeting}
}

func (g *greeter) Greet(name string) string {
	return g.prefix + ", " + name + "!"
}
