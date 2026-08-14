package handlers

import (
	"testing"

	"github.com/XiovV/calendar/server/internal/apptest"
	"github.com/XiovV/calendar/server/internal/config"
	"github.com/XiovV/calendar/server/internal/service"
)

// newTestGraph builds the repositories and services a handler test serves
// over, through the very service.NewGraph production runs on (#214,
// ADR-0065) — so a test never states the build order, and adding a
// repository never edits one.
//
// The handlers themselves stay hand-built per test: they are what these
// tests are about, and each is a single constructor call over the graph.
// (internal/app builds them too, but this package can't reach it — Go
// refuses an in-package test that imports a package importing the package
// under test, and app imports handlers.)
func newTestGraph(t *testing.T, opts ...service.GraphOption) *service.Graph {
	t.Helper()
	return newTestGraphWithConfig(t, apptest.Config(t), opts...)
}

// newTestGraphWithConfig is newTestGraph for a test that needs a setting
// other than apptest.Config's — a bootstrap account of its own choosing,
// say, or a lowered attachment limit.
func newTestGraphWithConfig(t *testing.T, cfg config.Config, opts ...service.GraphOption) *service.Graph {
	t.Helper()

	g, err := service.NewInMemoryGraph(cfg, opts...)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	return g
}
