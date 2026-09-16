// Package config holds the value the example injects into its container.
package config

// Config is passed to the container constructor rather than built by it, so
// every provider taking a Config gets this one.
type Config struct {
	Greeting string
	DataDir  string
}
