//go:build digen

package main

import "github.com/Nethron-Inc/digen"

// testContainer is the same wiring as appContainer, built for a test.
//
// digen.Include adds every provider of appContainer, so nothing is listed twice
// and the two cannot drift apart. What makes it a test container is its
// constructor's call site (newTestContainer in main.go): it is passed a
// greeting.Greeter, and a value passed in always wins over a provider of the
// same type. Everything that needs a Greeter, the console notifier included, is
// handed the test's one, while NewAppContainer still builds the real greeter.
var testContainer = digen.ProviderSet{
	digen.Include(appContainer),
}
