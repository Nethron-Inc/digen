// Package store provides the example's fallible component.
package store

import (
	"errors"

	"bugdrill.ai/digen/examples/basic/config"
)

// Store is where the audit notifier writes.
type Store interface {
	Path() string
}

type store struct {
	dir string
}

// NewStore can fail, so the container's accessor returns (Store, error) and the
// value is never proxied — the error has to surface at the accessor rather than
// on some later method call.
func NewStore(cfg config.Config) (Store, error) {
	if cfg.DataDir == "" {
		return nil, errors.New("store: DataDir is empty")
	}
	return &store{dir: cfg.DataDir}, nil
}

func (s *store) Path() string { return s.dir }
