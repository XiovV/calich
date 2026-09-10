package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/outbox"
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

	// Terminal, and explicitly not a delivery: the row records why it never
	// went out rather than claiming Google accepted it (ADR-0079).
	err = svc.SendWriteBack(ctx, msg)
	if !errors.Is(err, outbox.ErrSkipped) {
		t.Fatalf("expected a no-longer-writable Source to skip the push, got %v", err)
	}
	if len(google.patchRequests) != 0 {
		t.Fatalf("expected no PATCH sent to a read-only Source, got %d", len(google.patchRequests))
	}
}

// TestConnectionService_SendWriteBack_NoOpWhenEventDeletedSinceQueued and
// TestConnectionService_SendWriteBack_NoOpWhenNeverReachedTheProvider cover
// the two other "nothing left to push" outcomes SendWriteBack's own doc
// comment names — both terminal (mark skipped, with the reason), never a retry
// and never a claim of delivery (ADR-0079).
func TestConnectionService_SendWriteBack_NoOpWhenEventDeletedSinceQueued(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, g, _, _, _ := newTestConnectionServiceForWriteBack(t, google)

	msg, err := g.OutboxRepo.EnqueueWriteBack(context.Background(), "no-such-event")
	if err != nil {
		t.Fatalf("enqueue write-back: %v", err)
	}
	if err := svc.SendWriteBack(context.Background(), msg); !errors.Is(err, outbox.ErrSkipped) {
		t.Fatalf("expected a deleted event to skip rather than error or claim delivery, got %v", err)
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
	if err := connSvc.SendWriteBack(ctx, msg); !errors.Is(err, outbox.ErrSkipped) {
		t.Fatalf("expected a never-linked event to skip rather than error or claim delivery, got %v", err)
	}
	if len(google.patchRequests) != 0 {
		t.Fatalf("expected no PATCH sent for an event with no ExternalUID, got %d", len(google.patchRequests))
	}
}

// TestConnectionService_SendWriteBack_RetriesConflictWithFreshEtag covers
// #291's own core case: a 412 (Google's stale If-Match) triggers a refetch
// of the Provider's current copy, and the retried PATCH carries the fresh
// etag that refetch learned rather than the stale one that started the
// whole attempt.
func TestConnectionService_SendWriteBack_RetriesConflictWithFreshEtag(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")}}
	google.patchConflictFirstNCalls = 1
	google.getEventResponse = googleEventItem("google-evt-1", "Standup (changed at google)", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")
	google.getEventResponse["etag"] = `"etag-after-conflict"`
	google.patchResponseETag = "etag-after-retry"

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	if _, err := g.Events.Update(ctx, userID, master.ID, EventWrite{
		CalendarID: calendar.ID, Title: "Standup (renamed here)", Start: master.Start, End: master.End,
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

	if len(google.getEventRequests) != 1 {
		t.Fatalf("expected exactly one refetch after the conflict, got %d", len(google.getEventRequests))
	}
	if len(google.patchRequests) != 2 {
		t.Fatalf("expected exactly two PATCH attempts (the conflict plus the retry), got %d", len(google.patchRequests))
	}
	if google.patchRequests[0].IfMatch != `"etag-google-evt-1"` {
		t.Fatalf("expected the first attempt to carry the stored etag, got %q", google.patchRequests[0].IfMatch)
	}
	if google.patchRequests[1].IfMatch != `"etag-after-conflict"` {
		t.Fatalf("expected the retry to carry the fresh etag learned from the refetch, got %q", google.patchRequests[1].IfMatch)
	}
	// Our own field wins on retry (ADR-0075) — the local edit is what's
	// re-sent, not the Provider's concurrent title.
	if google.patchRequests[1].Body["summary"] != "Standup (renamed here)" {
		t.Fatalf("expected the retry to still carry our own edit, got %v", google.patchRequests[1].Body["summary"])
	}

	got, err := g.EventRepo.GetByID(ctx, master.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.ProviderEtag == nil || *got.ProviderEtag != "etag-after-retry" {
		t.Fatalf("expected the retry's own response etag applied, got %v", got.ProviderEtag)
	}
	if got.WriteBackError != nil {
		t.Fatalf("expected no permanent-failure marker after a resolved conflict, got %v", *got.WriteBackError)
	}
}

// TestConnectionService_SendWriteBack_ConflictKeepsBothSidesChangesToDifferentFields
// covers #291's own acceptance criterion directly: "an Event changed here
// and at the Provider in different fields keeps both changes". The local
// edit touches the title (a field this app owns and pushes); the Provider's
// concurrent change is to attendees (a field this app never pushes at all,
// ADR-0075) — after the conflict resolves, the retried PATCH still carries
// our title, and the Provider's guest count/RSVP survive locally, folded
// forward by the very refetch the conflict already required.
func TestConnectionService_SendWriteBack_ConflictKeepsBothSidesChangesToDifferentFields(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")}}
	google.patchConflictFirstNCalls = 1

	// The Provider's own concurrent change: a guest was added while this
	// app's edit was in flight. Not a field buildGooglePatch ever sends.
	google.getEventResponse = googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")
	google.getEventResponse["etag"] = `"etag-after-conflict"`
	google.getEventResponse["attendees"] = []map[string]any{
		{"self": true, "responseStatus": "accepted"},
		{"responseStatus": "needsAction"},
	}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	// Our own concurrent change: the title, a field this app does push.
	if _, err := g.Events.Update(ctx, userID, master.ID, EventWrite{
		CalendarID: calendar.ID, Title: "Standup (renamed here)", Start: master.Start, End: master.End,
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

	if len(google.patchRequests) != 2 {
		t.Fatalf("expected the conflict plus one retry, got %d PATCH attempts", len(google.patchRequests))
	}
	// Our change survives: the retried push still carries the local title.
	if google.patchRequests[1].Body["summary"] != "Standup (renamed here)" {
		t.Fatalf("expected our own title change to survive the conflict, got %v", google.patchRequests[1].Body["summary"])
	}
	// The Provider's change survives too: buildGooglePatch has no field for
	// attendees at all, so Google's own field-scoped PATCH semantics never
	// touch them regardless of what this app pushes.
	if _, hasAttendees := google.patchRequests[1].Body["attendees"]; hasAttendees {
		t.Fatalf("expected the push to carry no attendees field at all, got %v", google.patchRequests[1].Body["attendees"])
	}

	got, err := g.EventRepo.GetByID(ctx, master.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.Title != "Standup (renamed here)" {
		t.Fatalf("expected our own title change stored, got %q", got.Title)
	}
	if got.GuestCount != 1 {
		t.Fatalf("expected the Provider's concurrent guest folded forward by the conflict's own refetch, got %d", got.GuestCount)
	}
	if got.RSVPStatus == nil || *got.RSVPStatus != "accepted" {
		t.Fatalf("expected the Provider's own RSVP folded forward, got %v", got.RSVPStatus)
	}
}

// TestConnectionService_SendWriteBack_MarksPermanentlyFailedAfterThreeConflicts
// covers #291's bound: a conflict that survives three attempts in a row
// marks the Event and raises the Calendar's Source into needs-attention,
// rather than retrying forever.
func TestConnectionService_SendWriteBack_MarksPermanentlyFailedAfterThreeConflicts(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")}}
	google.patchConflictFirstNCalls = 10 // every attempt conflicts
	google.getEventResponse = googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")
	google.getEventResponse["etag"] = `"etag-after-conflict"`

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	if _, err := g.Events.Update(ctx, userID, master.ID, EventWrite{
		CalendarID: calendar.ID, Title: "Standup (renamed here)", Start: master.Start, End: master.End,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	pending := pendingWriteBacks(t, g, master.ID)
	if len(pending) != 1 {
		t.Fatalf("expected exactly one pending write-back, got %d", len(pending))
	}

	if err := svc.SendWriteBack(ctx, pending[0]); err != nil {
		t.Fatalf("expected an exhausted conflict to be handled rather than returned as an error, got %v", err)
	}
	if len(google.patchRequests) != maxWriteBackAttempts {
		t.Fatalf("expected exactly %d PATCH attempts, got %d", maxWriteBackAttempts, len(google.patchRequests))
	}

	got, err := g.EventRepo.GetByID(ctx, master.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.WriteBackError == nil {
		t.Fatalf("expected a permanent-failure marker on the Event after three conflicts")
	}

	source, err := g.SourceRepo.GetByCalendarID(ctx, calendar.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if source.ErrorClass == nil || *source.ErrorClass != ErrorClassNeedsAttention {
		t.Fatalf("expected the Source raised into needs-attention, got %v", source.ErrorClass)
	}
}

// TestConnectionService_SendWriteBack_ExpiresConnectionAndMarksFailedOnDeadRefreshToken
// covers #291's Connection-death case: the access token has died (401) and
// the stored refresh_token no longer works either (Google's invalid_grant),
// discovered mid-push. The Connection is recorded Expired, and the push is
// marked permanently failed immediately rather than retried by the outbox's
// ordinary backoff against a grant that's gone.
func TestConnectionService_SendWriteBack_ExpiresConnectionAndMarksFailedOnDeadRefreshToken(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")}}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	if _, err := g.Events.Update(ctx, userID, master.ID, EventWrite{
		CalendarID: calendar.ID, Title: "Standup (renamed here)", Start: master.Start, End: master.End,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	pending := pendingWriteBacks(t, g, master.ID)
	if len(pending) != 1 {
		t.Fatalf("expected exactly one pending write-back, got %d", len(pending))
	}

	// The cached access token has died, and Google now rejects the stored
	// refresh_token outright (RFC 6749 invalid_grant, a revoked or expired
	// grant) — importOneCalendar's own initial exchange already happened, so
	// flipping tokenStatus now only affects mintAccessToken's later refresh
	// call.
	google.patchStatus = http.StatusUnauthorized
	google.tokenStatus = http.StatusBadRequest

	if err := svc.SendWriteBack(ctx, pending[0]); err != nil {
		t.Fatalf("expected a dead connection to be handled rather than returned as an error, got %v", err)
	}

	linkedSource, err := g.SourceRepo.GetByCalendarID(ctx, calendar.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	conn, err := g.ConnectionRepo.GetByID(ctx, userID, *linkedSource.ConnectionID)
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if conn.Status != repository.ConnectionStatusExpired {
		t.Fatalf("expected the connection recorded expired, got %q", conn.Status)
	}

	got, err := g.EventRepo.GetByID(ctx, master.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.WriteBackError == nil {
		t.Fatalf("expected a permanent-failure marker on the Event after a dead connection")
	}
}

// TestOutboxDispatcher_HandleTerminalFailure_MarksPermanentlyFailedWriteBack
// covers #291's other permanent-failure path: an ordinary (non-conflict)
// failure — a 403 the calendar keeps answering — that the outbox's own
// backoff schedule eventually exhausts. Discovered by the Worker rather than
// inside one SendWriteBack call, so this exercises OutboxDispatcher's
// TerminalFailureHandler directly, the same hook the Worker calls.
func TestOutboxDispatcher_HandleTerminalFailure_MarksPermanentlyFailedWriteBack(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")}}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	msg, err := g.OutboxRepo.EnqueueWriteBack(ctx, master.ID)
	if err != nil {
		t.Fatalf("enqueue write-back: %v", err)
	}

	dispatcher := &OutboxDispatcher{Mail: NewInvitationSender(g.Events, nil, "calich@example.com"), WriteBack: svc}
	sendErr := errors.New("google says no")
	if err := dispatcher.HandleTerminalFailure(ctx, msg, sendErr); err != nil {
		t.Fatalf("handle terminal failure: %v", err)
	}

	got, err := g.EventRepo.GetByID(ctx, master.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.WriteBackError == nil {
		t.Fatalf("expected a permanent-failure marker on the Event")
	}

	src, err := g.SourceRepo.GetByCalendarID(ctx, calendar.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if src.ErrorClass == nil || *src.ErrorClass != ErrorClassNeedsAttention {
		t.Fatalf("expected the Source raised into needs-attention, got %v", src.ErrorClass)
	}

	// A mail message must never reach the write-back marking path.
	mailMsg := repository.OutboxMessage{Kind: repository.OutboxKindMail, EventID: master.ID}
	if err := dispatcher.HandleTerminalFailure(ctx, mailMsg, sendErr); err != nil {
		t.Fatalf("handle terminal failure for mail: %v", err)
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

// TestClassifyWriteBackFailure covers ADR-0079's retry classification: a Google
// 4xx describes a request that will be rejected identically every time, so it
// fails on the first response instead of walking the backoff schedule, while the
// three statuses that describe a moment rather than the request stay retryable.
// The original 400 this rule exists for — a malformed patch body — would
// otherwise have burned four retries over thirteen minutes before its Event
// showed any marker at all.
func TestClassifyWriteBackFailure(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		permanent bool
	}{
		{name: "400 is a body Google will reject identically forever", err: &googleHTTPError{sentinel: ErrGoogleWriteBackFailed, statusCode: http.StatusBadRequest}, permanent: true},
		{name: "401 is a grant that no longer authenticates", err: &googleHTTPError{sentinel: ErrGoogleWriteBackFailed, statusCode: http.StatusUnauthorized}, permanent: true},
		{name: "403 is a calendar this account may not write", err: &googleHTTPError{sentinel: ErrGoogleWriteBackFailed, statusCode: http.StatusForbidden}, permanent: true},
		{name: "404 is an event that is gone", err: &googleHTTPError{sentinel: ErrGoogleWriteBackFailed, statusCode: http.StatusNotFound}, permanent: true},
		{name: "408 describes a moment, not the request", err: &googleHTTPError{sentinel: ErrGoogleWriteBackFailed, statusCode: http.StatusRequestTimeout}},
		{name: "429 is the one where waiting is the remedy", err: &googleHTTPError{sentinel: ErrGoogleWriteBackFailed, statusCode: http.StatusTooManyRequests}},
		{name: "412 belongs to SendWriteBack's own refetch loop", err: &googleHTTPError{sentinel: ErrGoogleWriteBackFailed, statusCode: http.StatusPreconditionFailed}},
		{name: "500 is the outage backoff was built for", err: &googleHTTPError{sentinel: ErrGoogleWriteBackFailed, statusCode: http.StatusInternalServerError}},
		{name: "503 likewise", err: &googleHTTPError{sentinel: ErrGoogleWriteBackFailed, statusCode: http.StatusServiceUnavailable}},
		{name: "a transport error carries no status to classify", err: errors.New("dial tcp: connection refused")},
		{name: "a skip is not a failure and must not be reclassified", err: skipWriteBack("this calendar no longer exists here")},
		{name: "nil stays nil", err: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyWriteBackFailure(tc.err)
			if errors.Is(got, outbox.ErrPermanent) != tc.permanent {
				t.Fatalf("permanent = %v, want %v (got %v)", !tc.permanent, tc.permanent, got)
			}
			// Whatever the classification, the original error must survive it —
			// the outbox row's last_error is where Google's own reason lands.
			if tc.err != nil && !errors.Is(got, tc.err) {
				t.Fatalf("expected the original error preserved, got %v", got)
			}
		})
	}
}

// TestGoogleHTTPError_CarriesGooglesOwnReason covers the observability half of
// ADR-0079. Every call in google.go used to drain a failed response into
// io.Discard under a comment asserting it "carries nothing we need on failure",
// which is exactly backwards for a 4xx: Google's body names the offending field,
// and without it an outbox row reads "status 400" and explains nothing.
func TestGoogleHTTPError_CarriesGooglesOwnReason(t *testing.T) {
	withBody := &googleHTTPError{
		sentinel:   ErrGoogleWriteBackFailed,
		statusCode: http.StatusBadRequest,
		body:       `{"error":{"message":"Invalid value for: Invalid format: \"\""}}`,
	}
	if !strings.Contains(withBody.Error(), "Invalid value for") {
		t.Fatalf("expected Google's own reason in the error text, got %q", withBody.Error())
	}
	if !strings.Contains(withBody.Error(), "status 400") {
		t.Fatalf("expected the status kept alongside the reason, got %q", withBody.Error())
	}

	// A response with no body reads exactly as it did before, rather than
	// trailing an empty separator.
	bare := &googleHTTPError{sentinel: ErrGoogleWriteBackFailed, statusCode: http.StatusForbidden}
	if got, want := bare.Error(), ErrGoogleWriteBackFailed.Error()+": status 403"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

// TestReadGoogleErrorBody_IsBoundedAndSingleLine covers the two properties that
// make storing a Provider's error body safe: a proxy or captive portal answering
// with a large HTML page cannot flood last_error, and Google's pretty-printed
// JSON collapses to one line for a column read in a terminal and a grid tooltip.
func TestReadGoogleErrorBody_IsBoundedAndSingleLine(t *testing.T) {
	got := readGoogleErrorBody(strings.NewReader("{\n  \"error\": {\n    \"message\": \"bad\"\n  }\n}"))
	if want := `{ "error": { "message": "bad" } }`; got != want {
		t.Fatalf("expected whitespace collapsed to one line\n got: %q\nwant: %q", got, want)
	}

	flood := readGoogleErrorBody(strings.NewReader(strings.Repeat("x", googleErrorBodyLimit*4)))
	if len(flood) != googleErrorBodyLimit {
		t.Fatalf("expected the body bounded to %d bytes, got %d", googleErrorBodyLimit, len(flood))
	}
}
