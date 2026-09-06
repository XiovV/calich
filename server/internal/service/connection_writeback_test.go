package service

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// newTestConnectionServiceForWriteBack is newTestConnectionServiceWithWorkspace
// plus the Graph itself — a write-back test needs direct repository access
// (seeding an outbox row, reading back the Event's stored ProviderEtag) that
// ConnectionService's own exported surface has no reason to offer.
func newTestConnectionServiceForWriteBack(t *testing.T, google *fakeGoogleServer) (svc *ConnectionService, g *Graph, auth *AuthService, userID, workspaceID int64) {
	t.Helper()

	g = newTestGraph(t)
	user, err := g.UserRepo.Create(context.Background(), "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspace, err := g.WorkspaceRepo.Create(context.Background(), "Test Workspace", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := g.WorkspaceRepo.AddMember(context.Background(), workspace.ID, user.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	connections := repository.NewConnectionRepository(g.DB)
	svc = NewConnectionService(connections, g.Auth, g.Calendars, g.Events, "test-client-id", "test-client-secret", "test-encryption-key", true,
		withGoogleHTTPClient(google.Client()),
		withGoogleEndpoints(google.URL+"/authorize", google.URL+"/token", google.URL+"/userinfo", google.URL+"/calendarList"),
		withGoogleEventsURL(google.URL),
	)
	return svc, g, g.Auth, user.ID, workspace.ID
}

// soleMasterOf returns calendarID's one stored Master row — every write-back
// test in this file imports exactly one Event, so there is exactly one to
// find.
func soleMasterOf(t *testing.T, g *Graph, calendarID string) repository.Event {
	t.Helper()
	events, err := g.EventRepo.ListByCalendarIDs(context.Background(), []string{calendarID}, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one imported event, got %d", len(events))
	}
	return events[0]
}

// TestConnectionService_SendWriteBack_PushesToProviderAndAppliesEcho is
// #290's end-to-end case: editing a Master on a writable Linked Calendar
// enqueues a push, and draining it reaches the Provider carrying the
// If-Match etag Refresh stored, then applies the response as though it were
// a Refresh result (ADR-0075, ADR-0076) — this app's own edit doesn't come
// back as a change on the next Delta Refresh.
func TestConnectionService_SendWriteBack_PushesToProviderAndAppliesEcho(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")}}
	google.patchResponseETag = "etag-after-push"

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	if master.ExternalUID == nil || *master.ExternalUID != "google-evt-1" {
		t.Fatalf("expected the imported Master to carry its Provider id, got %+v", master.ExternalUID)
	}

	if _, err := g.Events.Update(ctx, userID, master.ID, EventWrite{
		CalendarID: calendar.ID, Title: "Standup (renamed)", Start: master.Start, End: master.End,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	pending := pendingWriteBacks(t, g, master.ID)
	if len(pending) != 1 {
		t.Fatalf("expected exactly one pending write-back, got %d", len(pending))
	}

	if err := svc.SendWriteBack(ctx, pending[0]); err != nil {
		t.Fatalf("send write-back: %v", err)
	}

	if len(google.patchRequests) != 1 {
		t.Fatalf("expected exactly one PATCH to reach the provider, got %d", len(google.patchRequests))
	}
	push := google.patchRequests[0]
	if push.CalendarID != "primary" || push.EventID != "google-evt-1" {
		t.Fatalf("expected the push addressed at primary/google-evt-1, got %+v", push)
	}
	if push.IfMatch != `"etag-google-evt-1"` {
		t.Fatalf("expected If-Match carrying the stored provider etag, got %q", push.IfMatch)
	}
	if push.Body["summary"] != "Standup (renamed)" {
		t.Fatalf("expected the renamed title in the push body, got %v", push.Body["summary"])
	}

	got, err := g.EventRepo.GetByID(ctx, master.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.ProviderEtag == nil || *got.ProviderEtag != "etag-after-push" {
		t.Fatalf("expected the push response's fresh etag applied as a Refresh result, got %v", got.ProviderEtag)
	}
}

// TestConnectionService_SendWriteBack_RefusesReadOnlySourceWithoutCallingGoogle
// covers the case a queued push races an ACL change: by the time it's
// drained, a Refresh already flipped the Source back to read-only. Nothing
// should reach Google for a Calendar this app itself would now refuse the
// edit on.
func TestConnectionService_SendWriteBack_RefusesReadOnlySourceWithoutCallingGoogle(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "reader", true)}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")}}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	// A read-only Source refuses Update itself (see event_writeback_test.go's
	// own coverage of that), so a queued row here can only arise from a race —
	// simulated directly against the outbox rather than through Update.
	msg, err := g.OutboxRepo.EnqueueWriteBack(ctx, master.ID)
	if err != nil {
		t.Fatalf("enqueue write-back: %v", err)
	}

	if err := svc.SendWriteBack(ctx, msg); err != nil {
		t.Fatalf("expected SendWriteBack to treat a no-longer-writable Source as done, got %v", err)
	}
	if len(google.patchRequests) != 0 {
		t.Fatalf("expected no PATCH sent to a read-only Source, got %d", len(google.patchRequests))
	}
}

// TestConnectionService_SendWriteBack_NoOpWhenEventDeletedSinceQueued and
// TestConnectionService_SendWriteBack_NoOpWhenNeverReachedTheProvider cover
// the two other "nothing left to push" outcomes SendWriteBack's own doc
// comment names — both a success (mark sent), never a retry.
func TestConnectionService_SendWriteBack_NoOpWhenEventDeletedSinceQueued(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, g, _, _, _ := newTestConnectionServiceForWriteBack(t, google)

	msg, err := g.OutboxRepo.EnqueueWriteBack(context.Background(), "no-such-event")
	if err != nil {
		t.Fatalf("enqueue write-back: %v", err)
	}
	if err := svc.SendWriteBack(context.Background(), msg); err != nil {
		t.Fatalf("expected a deleted event to no-op rather than error, got %v", err)
	}
	if len(google.patchRequests) != 0 {
		t.Fatalf("expected no PATCH sent for a deleted event, got %d", len(google.patchRequests))
	}
}

func TestConnectionService_SendWriteBack_NoOpWhenNeverReachedTheProvider(t *testing.T) {
	ctx := context.Background()
	g := newTestGraph(t)
	svc, userID, calendarID := newTestEventServiceOnGraph(t, g)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	// An ordinary, entirely local Event — never touched by a Refresh, so its
	// ExternalUID is nil (Create's own concern, #292, doesn't exist yet).
	event, err := svc.Create(ctx, userID, "evt-local-only", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	google := newFakeGoogleServer(t)
	connSvc, _, _, _, _ := newTestConnectionServiceForWriteBack(t, google)

	msg, err := g.OutboxRepo.EnqueueWriteBack(ctx, event.ID)
	if err != nil {
		t.Fatalf("enqueue write-back: %v", err)
	}
	if err := connSvc.SendWriteBack(ctx, msg); err != nil {
		t.Fatalf("expected a never-linked event to no-op rather than error, got %v", err)
	}
	if len(google.patchRequests) != 0 {
		t.Fatalf("expected no PATCH sent for an event with no ExternalUID, got %d", len(google.patchRequests))
	}
}

// TestOutboxDispatcher_Send_RoutesByKind covers the dispatcher's one job:
// picking the right sender by msg.Kind, without the outbox Worker itself
// knowing Kind exists (outbox/worker.go).
func TestOutboxDispatcher_Send_RoutesByKind(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")}}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	dispatcher := &OutboxDispatcher{
		Mail:      NewInvitationSender(g.Events, nil, "calich@example.com"),
		WriteBack: svc,
	}

	writebackMsg, err := g.OutboxRepo.EnqueueWriteBack(context.Background(), master.ID)
	if err != nil {
		t.Fatalf("enqueue write-back: %v", err)
	}
	if err := dispatcher.Send(context.Background(), writebackMsg); err != nil {
		t.Fatalf("dispatch write-back: %v", err)
	}
	if len(google.patchRequests) != 1 {
		t.Fatalf("expected the write-back kind routed to Google, got %d PATCH requests", len(google.patchRequests))
	}
}

// TestConnectionService_FullRefresh_NeverRevertsAnEventWithAPendingWriteBack
// is #290's own end-to-end ADR-0076 regression: the multi-agent review's
// most serious finding was that a Full Refresh landing in the window between
// a local edit committing and its push actually reaching Google would
// silently revert that edit back to Google's still-stale copy. Google's own
// fixture is deliberately left showing the *pre-edit* content throughout —
// this is exactly what a real interleaving looks like, since the push that
// would update Google hasn't been drained yet.
func TestConnectionService_FullRefresh_NeverRevertsAnEventWithAPendingWriteBack(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York")},
	}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	masters, _, err := svc.events.ListSeriesByCalendar(ctx, userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 1 {
		t.Fatalf("expected exactly one imported master, got %d", len(masters))
	}
	master := masters[0]

	// The local edit — committed, but its push is still sitting Pending in
	// the outbox, not yet drained.
	if _, err := svc.events.Update(ctx, userID, master.ID, EventWrite{
		CalendarID: calendar.ID, Title: "Dentist (local edit, not yet pushed)", Start: master.Start, End: master.End,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	// A Full Refresh runs before the push ever reaches Google — Google's own
	// fixture still answers with the pre-edit title.
	result, err := svc.FullRefresh(ctx, userID, calendar.ID)
	if err != nil {
		t.Fatalf("full refresh: %v", err)
	}
	if result.Updated != 0 {
		t.Fatalf("expected the pending series to reconcile as untouched, got %+v", result)
	}

	got, err := svc.events.GetByIDUnchecked(ctx, master.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.Title != "Dentist (local edit, not yet pushed)" {
		t.Fatalf("expected the local edit to survive the Refresh, got title %q", got.Title)
	}
}
