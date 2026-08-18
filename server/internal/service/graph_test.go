package service

import (
	"testing"

	"github.com/XiovV/calich/server/internal/apptest"
	"github.com/XiovV/calich/server/internal/config"
)

// newTestGraph builds this package's repositories and services over a fresh
// in-memory database, through the very NewGraph production runs on (#214,
// ADR-0065) — so a test never states the build order, and adding a
// repository never edits one.
//
// Seeding stays with the test: what Users, Workspaces, Calendars and Events
// a case needs is part of that case, not of construction.
func newTestGraph(t *testing.T, opts ...GraphOption) *Graph {
	t.Helper()
	return newTestGraphWithConfig(t, apptest.Config(t), opts...)
}

// newTestGraphWithConfig is newTestGraph for a test that needs a setting
// other than apptest.Config's — a lowered invite rate limit, an SMTP
// transport, a bootstrap account of its own choosing.
func newTestGraphWithConfig(t *testing.T, cfg config.Config, opts ...GraphOption) *Graph {
	t.Helper()

	g, err := NewInMemoryGraph(cfg, opts...)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	return g
}
