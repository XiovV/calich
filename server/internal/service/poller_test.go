package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// fakeDueSubscriptionLister returns a fixed set of due Calendars, standing
// in for CalendarService.ListDueForRefresh in Poller tests that don't need
// real persistence.
type fakeDueSubscriptionLister struct {
	due []repository.Calendar
	// lastNow captures the argument the last ListDueForRefresh call
	// received, so a test can assert Poller passed its own clock through.
	lastNow time.Time
}

func (f *fakeDueSubscriptionLister) ListDueForRefresh(_ context.Context, now time.Time) ([]repository.Calendar, error) {
	f.lastNow = now
	return f.due, nil
}

// fakeRefresher records every (userID, calendarID) it's asked to refresh,
// standing in for SubscribeService.Refresh — no real fetch or reconcile.
type fakeRefresher struct {
	calls []string
	// failFor, when non-nil, makes Refresh error for that one calendarID.
	failFor map[string]error
}

func (f *fakeRefresher) Refresh(_ context.Context, userID int64, calendarID string, force bool) (RefreshResult, error) {
	f.calls = append(f.calls, calendarID)
	if force {
		panic("poller must never force a refresh — that would bypass backoff")
	}
	if err, ok := f.failFor[calendarID]; ok {
		return RefreshResult{}, err
	}
	return RefreshResult{}, nil
}

func TestPoller_Tick_RefreshesEveryDueSubscription(t *testing.T) {
	lister := &fakeDueSubscriptionLister{due: []repository.Calendar{
		{ID: "cal-1", UserID: 1},
		{ID: "cal-2", UserID: 1},
	}}
	refresher := &fakeRefresher{}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	poller := NewPoller(lister, refresher, func() time.Time { return now })

	if err := poller.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(refresher.calls) != 2 {
		t.Fatalf("expected 2 refresh calls, got %+v", refresher.calls)
	}
	if !lister.lastNow.Equal(now) {
		t.Fatalf("expected the poller's own clock to be passed to ListDueForRefresh, got %v", lister.lastNow)
	}
}

func TestPoller_Tick_NoDueSubscriptionsRefreshesNothing(t *testing.T) {
	lister := &fakeDueSubscriptionLister{}
	refresher := &fakeRefresher{}
	poller := NewPoller(lister, refresher, time.Now)

	if err := poller.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(refresher.calls) != 0 {
		t.Fatalf("expected no refresh calls, got %+v", refresher.calls)
	}
}

// A broken feed's Refresh error must not stop the pass — every other due
// Subscription still gets its turn.
func TestPoller_Tick_OneFailingRefreshDoesNotBlockOthers(t *testing.T) {
	lister := &fakeDueSubscriptionLister{due: []repository.Calendar{
		{ID: "cal-broken", UserID: 1},
		{ID: "cal-healthy", UserID: 1},
	}}
	refresher := &fakeRefresher{failFor: map[string]error{"cal-broken": errors.New("boom")}}
	poller := NewPoller(lister, refresher, time.Now)

	if err := poller.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(refresher.calls) != 2 {
		t.Fatalf("expected both calendars to be attempted, got %+v", refresher.calls)
	}
}

func TestPoller_Tick_NeverForcesARefresh(t *testing.T) {
	lister := &fakeDueSubscriptionLister{due: []repository.Calendar{{ID: "cal-1", UserID: 1}}}
	refresher := &fakeRefresher{}
	poller := NewPoller(lister, refresher, time.Now)

	// fakeRefresher.Refresh panics if force is ever true — Tick surviving
	// this call is the assertion.
	if err := poller.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
}

func TestPoller_Tick_ListErrorIsPropagated(t *testing.T) {
	poller := NewPoller(failingLister{}, &fakeRefresher{}, time.Now)

	if err := poller.Tick(context.Background()); err == nil {
		t.Fatal("expected the list error to propagate")
	}
}

type failingLister struct{}

func (failingLister) ListDueForRefresh(context.Context, time.Time) ([]repository.Calendar, error) {
	return nil, errors.New("db is down")
}
