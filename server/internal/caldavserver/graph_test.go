package caldavserver

import (
	"testing"

	"github.com/XiovV/calendar/server/internal/apptest"
	"github.com/XiovV/calendar/server/internal/config"
	"github.com/XiovV/calendar/server/internal/service"
)

// newTestGraph builds the repositories and services the CalDAV Backend
// serves over, through the very service.NewGraph production runs on (#214,
// ADR-0065) — so a test never states the build order, and adding a
// repository never edits one.
//
// The Backend itself stays hand-built: it is what these tests are about,
// and it is one constructor call over the graph. (internal/app builds one
// too, but this package can't reach it — Go refuses an in-package test that
// imports a package importing the package under test, and app imports
// caldavserver.)
func newTestGraph(t *testing.T, opts ...service.GraphOption) *service.Graph {
	t.Helper()
	return newTestGraphWithConfig(t, apptest.Config(t), opts...)
}

func newTestGraphWithConfig(t *testing.T, cfg config.Config, opts ...service.GraphOption) *service.Graph {
	t.Helper()

	g, err := service.NewInMemoryGraph(cfg, opts...)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	return g
}
