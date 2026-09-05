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
	due []repository.DueRefresh
	// lastNow captures the argument the last ListDueForRefresh call
	// received, so a test can assert Poller passed its own clock through.
	lastNow time.Time
}

func (f *fakeDueSubscriptionLister) ListDueForRefresh(_ context.Context, now time.Time) ([]repository.DueRefresh, error) {
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

// fakeLinkedRefresher records the mode each Linked Calendar Refresh was
// asked for, standing in for ConnectionService.RefreshLinked.
type fakeLinkedRefresher struct {
	modes   map[string]RefreshMode
	failFor map[string]error
}

func (f *fakeLinkedRefresher) RefreshLinked(_ context.Context, userID int64, calendarID string, mode RefreshMode) (RefreshResult, error) {
	if f.modes == nil {
		f.modes = map[string]RefreshMode{}
	}
	f.modes[calendarID] = mode
	if err, ok := f.failFor[calendarID]; ok {
		return RefreshResult{}, err
	}
	return RefreshResult{}, nil
}

func newTestPoller(lister DueSourceLister, sub SubscriptionRefresher, linked LinkedCalendarRefresher) *Poller {
	if sub == nil {
		sub = &fakeRefresher{}
	}
	if linked == nil {
		linked = &fakeLinkedRefresher{}
	}
	return NewPoller(lister, sub, linked, time.Now)
}

func TestPoller_Tick_RefreshesEveryDueSubscription(t *testing.T) {
	lister := &fakeDueSubscriptionLister{due: []repository.DueRefresh{
		{CalendarID: "cal-1", UserID: 1, Kind: repository.SourceKindSubscription},
		{CalendarID: "cal-2", UserID: 1, Kind: repository.SourceKindSubscription},
	}}
	refresher := &fakeRefresher{}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	poller := NewPoller(lister, refresher, &fakeLinkedRefresher{}, func() time.Time { return now })

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
	poller := newTestPoller(lister, refresher, nil)

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
	lister := &fakeDueSubscriptionLister{due: []repository.DueRefresh{
		{CalendarID: "cal-broken", UserID: 1, Kind: repository.SourceKindSubscription},
		{CalendarID: "cal-healthy", UserID: 1, Kind: repository.SourceKindSubscription},
	}}
	refresher := &fakeRefresher{failFor: map[string]error{"cal-broken": errors.New("boom")}}
	poller := newTestPoller(lister, refresher, nil)

	if err := poller.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(refresher.calls) != 2 {
		t.Fatalf("expected both calendars to be attempted, got %+v", refresher.calls)
	}
}

func TestPoller_Tick_NeverForcesARefresh(t *testing.T) {
	lister := &fakeDueSubscriptionLister{due: []repository.DueRefresh{{CalendarID: "cal-1", UserID: 1, Kind: repository.SourceKindSubscription}}}
	refresher := &fakeRefresher{}
	poller := newTestPoller(lister, refresher, nil)

	// fakeRefresher.Refresh panics if force is ever true — Tick surviving
	// this call is the assertion.
	if err := poller.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
}

func TestPoller_Tick_ListErrorIsPropagated(t *testing.T) {
	poller := newTestPoller(failingLister{}, &fakeRefresher{}, nil)

	if err := poller.Tick(context.Background()); err == nil {
		t.Fatal("expected the list error to propagate")
	}
}

// A Connection-kind due Source is routed to the Linked Calendar refresher,
// and the mode is Delta when it already holds a cursor, Full when it
// doesn't (#288, ADR-0053).
func TestPoller_Tick_RoutesLinkedCalendarsByCursor(t *testing.T) {
	lister := &fakeDueSubscriptionLister{due: []repository.DueRefresh{
		{CalendarID: "linked-fresh", UserID: 1, Kind: repository.SourceKindConnection, HasCursor: false},
		{CalendarID: "linked-cursored", UserID: 1, Kind: repository.SourceKindConnection, HasCursor: true},
		{CalendarID: "sub", UserID: 1, Kind: repository.SourceKindSubscription},
	}}
	sub := &fakeRefresher{}
	linked := &fakeLinkedRefresher{}
	poller := NewPoller(lister, sub, linked, time.Now)

	if err := poller.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if got := linked.modes["linked-fresh"]; got != RefreshModeFull {
		t.Fatalf("expected a cursor-less Linked Calendar to run Full, got %v", got)
	}
	if got := linked.modes["linked-cursored"]; got != RefreshModeDelta {
		t.Fatalf("expected a cursored Linked Calendar to run Delta, got %v", got)
	}
	if len(sub.calls) != 1 || sub.calls[0] != "sub" {
		t.Fatalf("expected the Subscription to route to the subscription refresher, got %+v", sub.calls)
	}
}

// One broken Linked Calendar's Refresh error must not stop the pass.
func TestPoller_Tick_OneFailingLinkedRefreshDoesNotBlockOthers(t *testing.T) {
	lister := &fakeDueSubscriptionLister{due: []repository.DueRefresh{
		{CalendarID: "linked-broken", UserID: 1, Kind: repository.SourceKindConnection, HasCursor: true},
		{CalendarID: "linked-healthy", UserID: 1, Kind: repository.SourceKindConnection, HasCursor: true},
	}}
	linked := &fakeLinkedRefresher{failFor: map[string]error{"linked-broken": errors.New("boom")}}
	poller := NewPoller(lister, &fakeRefresher{}, linked, time.Now)

	if err := poller.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if _, ok := linked.modes["linked-healthy"]; !ok {
		t.Fatalf("expected the healthy Linked Calendar to still be attempted, got %+v", linked.modes)
	}
}

type failingLister struct{}

func (failingLister) ListDueForRefresh(context.Context, time.Time) ([]repository.DueRefresh, error) {
	return nil, errors.New("db is down")
}
