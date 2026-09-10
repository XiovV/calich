package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/outbox"
	"github.com/XiovV/calich/server/internal/repository"
)

// TestEventService_Create_EnqueuesCreateWriteBackOnWritableLinkedCalendar
// covers #292's first acceptance-criteria bullet at the enqueue seam:
// creating a plain Master on a writable Linked Calendar succeeds locally
// (with no ExternalUID yet) and queues an events.insert push alongside it.
func TestEventService_Create_EnqueuesCreateWriteBackOnWritableLinkedCalendar(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	ctx := context.Background()
	start := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 9, 30, 0, 0, time.UTC)

	created, err := g.Events.Create(ctx, userID, "evt-new-linked", EventWrite{
		CalendarID: calendarID, Title: "New meeting", Start: start, End: end,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ExternalUID != nil {
		t.Fatalf("expected a brand-new local Event to carry no Provider id yet, got %v", *created.ExternalUID)
	}

	pending := pendingWriteBacks(t, g, created.ID)
	if len(pending) != 1 {
		t.Fatalf("expected exactly one pending write-back, got %d", len(pending))
	}
	if pending[0].Method != repository.OutboxMethodPost {
		t.Fatalf("expected method %q, got %q", repository.OutboxMethodPost, pending[0].Method)
	}
}

// TestEventService_Create_RefusesOnLinkedCalendarWhoseConnectionNeedsReconnect
// mirrors Update's own #291 rule for the create path: a Connection off Live
// refuses a new create outright rather than committing it locally and
// queuing a push that can never land.
func TestEventService_Create_RefusesOnLinkedCalendarWhoseConnectionNeedsReconnect(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	ctx := context.Background()

	source, err := g.SourceRepo.GetByCalendarID(ctx, calendarID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if err := g.ConnectionRepo.UpdateStatus(ctx, userID, *source.ConnectionID, repository.ConnectionStatusExpired); err != nil {
		t.Fatalf("update connection status: %v", err)
	}

	_, err = g.Events.Create(ctx, userID, "evt-new-dead-conn", EventWrite{
		CalendarID: calendarID, Title: "Nope", Start: time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 2, 1, 9, 30, 0, 0, time.UTC),
	})
	if err != ErrConnectionNeedsReconnect {
		t.Fatalf("expected ErrConnectionNeedsReconnect, got %v", err)
	}
	if _, err := g.EventRepo.GetByID(ctx, "evt-new-dead-conn"); err != repository.ErrNotFound {
		t.Fatalf("expected no local row written for a refused create, got %v", err)
	}
}

// TestEventService_Delete_SupersedesAPendingCreateWriteBack covers
// ADR-0077's supersede rule and its create/delete-race close: an Event
// created locally and deleted before its events.insert ever drained has the
// pending POST dropped and a single DELETE queued against the deterministic
// Provider id — so an insert that landed anyway (or one still in flight) is
// cleaned up, and a never-sent one no-ops on Google's 404.
func TestEventService_Delete_SupersedesAPendingCreateWriteBack(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	ctx := context.Background()

	created, err := g.Events.Create(ctx, userID, "evt-created-then-deleted", EventWrite{
		CalendarID: calendarID, Title: "Ephemeral", Start: time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 2, 1, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p := pendingWriteBacks(t, g, created.ID); len(p) != 1 || p[0].Method != repository.OutboxMethodPost {
		t.Fatalf("expected exactly one pending POST, got %+v", p)
	}

	if err := g.Events.Delete(ctx, userID, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	pending := pendingWriteBacks(t, g, created.ID)
	if len(pending) != 1 || pending[0].Method != repository.OutboxMethodDelete {
		t.Fatalf("expected the POST dropped and one DELETE queued, got %+v", pending)
	}
	if pending[0].WriteBackDelete == nil || pending[0].WriteBackDelete.ExternalUID != googleClientEventID(created.ID) {
		t.Fatalf("expected the DELETE snapshot to carry the deterministic Provider id, got %+v", pending[0].WriteBackDelete)
	}
}

// TestEventService_Update_ReEnqueuesCreateWriteBackForANeverPushedMaster
// covers ADR-0077's retry path: editing a locally created Master whose
// events.insert hasn't drained (or permanently failed) re-enqueues the POST
// and clears any stale failure marker, rather than silently doing nothing.
func TestEventService_Update_ReEnqueuesCreateWriteBackForANeverPushedMaster(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	ctx := context.Background()
	start := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 9, 30, 0, 0, time.UTC)

	created, err := g.Events.Create(ctx, userID, "evt-retry-create", EventWrite{
		CalendarID: calendarID, Title: "First title", Start: start, End: end,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// The create push permanently failed and its row is terminal.
	pending := pendingWriteBacks(t, g, created.ID)
	if err := g.OutboxRepo.MarkFailed(ctx, pending[0].ID, 5, "google said no"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if err := g.EventRepo.MarkWriteBackFailed(ctx, created.ID, "google said no"); err != nil {
		t.Fatalf("mark event failed: %v", err)
	}

	if _, err := g.Events.Update(ctx, userID, created.ID, EventWrite{
		CalendarID: calendarID, Title: "Edited title", Start: start, End: end,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	again := pendingWriteBacks(t, g, created.ID)
	if len(again) != 1 || again[0].Method != repository.OutboxMethodPost {
		t.Fatalf("expected a fresh POST write-back re-enqueued, got %+v", again)
	}
	got, err := g.EventRepo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.WriteBackError != nil {
		t.Fatalf("expected the stale marker cleared by the fresh edit, got %v", *got.WriteBackError)
	}
}

// TestConnectionService_SendWriteBack_CreatePushesInsertAndAdoptsProviderID
// is #292's create acceptance criterion end to end: draining the queued POST
// reaches events.insert carrying the field-scoped body, and the id Google
// returns is adopted onto the local row so the next Refresh reconciles by it.
func TestConnectionService_SendWriteBack_CreatePushesInsertAndAdoptsProviderID(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {}}
	google.insertResponseETag = "etag-after-insert"

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	created, err := g.Events.Create(ctx, userID, "evt-created-locally", EventWrite{
		CalendarID: calendar.ID, Title: "Kickoff", Start: time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC), End: time.Date(2026, 3, 2, 16, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	pending := pendingWriteBacks(t, g, created.ID)
	if len(pending) != 1 {
		t.Fatalf("expected exactly one pending write-back, got %d", len(pending))
	}
	if err := svc.SendWriteBack(ctx, pending[0]); err != nil {
		t.Fatalf("send write-back: %v", err)
	}

	if len(google.insertRequests) != 1 {
		t.Fatalf("expected exactly one events.insert, got %d", len(google.insertRequests))
	}
	if google.insertRequests[0].CalendarID != "primary" {
		t.Fatalf("expected the insert addressed at primary, got %q", google.insertRequests[0].CalendarID)
	}
	if google.insertRequests[0].Body["summary"] != "Kickoff" {
		t.Fatalf("expected the created title in the insert body, got %v", google.insertRequests[0].Body["summary"])
	}
	if google.insertRequests[0].Body["id"] != googleClientEventID(created.ID) {
		t.Fatalf("expected the insert to carry a deterministic client id, got %v", google.insertRequests[0].Body["id"])
	}
	if _, hasAttendees := google.insertRequests[0].Body["attendees"]; hasAttendees {
		t.Fatalf("expected the field-scoped body to carry no attendees, got %v", google.insertRequests[0].Body["attendees"])
	}

	got, err := g.EventRepo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.ExternalUID == nil || *got.ExternalUID != googleClientEventID(created.ID) {
		t.Fatalf("expected the Provider id adopted onto the row, got %v", got.ExternalUID)
	}
	if got.ProviderEtag == nil || *got.ProviderEtag != "etag-after-insert" {
		t.Fatalf("expected the insert response etag adopted, got %v", got.ProviderEtag)
	}
}

// TestConnectionService_SendWriteBack_CreateIsIdempotentOnRetry covers the
// review's finding: an events.insert that reached Google but whose response
// was lost must not create a duplicate on retry. The client-supplied id
// makes the second POST answer 409, which insertEvent resolves to the
// existing event.
func TestConnectionService_SendWriteBack_CreateIsIdempotentOnRetry(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {}}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	created, err := g.Events.Create(ctx, userID, "evt-idempotent-create", EventWrite{
		CalendarID: calendar.ID, Title: "Once", Start: time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC), End: time.Date(2026, 3, 2, 16, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pending := pendingWriteBacks(t, g, created.ID)

	// First drain: Google commits the insert, then the response is lost.
	google.insertCommitThenFailStatus = http.StatusInternalServerError
	if err := svc.SendWriteBack(ctx, pending[0]); err == nil {
		t.Fatalf("expected the lost-response insert to surface an error for the outbox to retry")
	}
	if got, _ := g.EventRepo.GetByID(ctx, created.ID); got.ExternalUID != nil {
		t.Fatalf("expected no id adopted after the failed insert, got %v", *got.ExternalUID)
	}

	// Retry: the same client id answers 409, resolved to the existing event.
	google.insertCommitThenFailStatus = 0
	if err := svc.SendWriteBack(ctx, pending[0]); err != nil {
		t.Fatalf("send write-back (retry): %v", err)
	}

	if len(google.deleteRequests) != 0 {
		t.Fatalf("expected no compensating delete, got %d", len(google.deleteRequests))
	}
	if len(google.insertedEvents) != 1 {
		t.Fatalf("expected exactly one event created at the provider despite the retry, got %d", len(google.insertedEvents))
	}
	got, err := g.EventRepo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.ExternalUID == nil || *got.ExternalUID != googleClientEventID(created.ID) {
		t.Fatalf("expected the deterministic id still adopted after the retry, got %v", got.ExternalUID)
	}
}

// TestConnectionService_SendWriteBack_DeletePushesToProviderUsingSnapshot is
// #292's delete acceptance criterion end to end: deleting a Master queues an
// events.delete whose snapshot addresses the Provider event (the local row
// is gone), and draining it reaches Google carrying the If-Match etag.
func TestConnectionService_SendWriteBack_DeletePushesToProviderUsingSnapshot(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")},
	}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	if err := g.Events.Delete(ctx, userID, master.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := g.EventRepo.GetByID(ctx, master.ID); err != repository.ErrNotFound {
		t.Fatalf("expected the local row removed, got %v", err)
	}

	pending := pendingWriteBacks(t, g, master.ID)
	if len(pending) != 1 || pending[0].Method != repository.OutboxMethodDelete {
		t.Fatalf("expected exactly one pending DELETE write-back, got %+v", pending)
	}
	if pending[0].WriteBackDelete == nil || pending[0].WriteBackDelete.ExternalUID != "google-evt-1" {
		t.Fatalf("expected the delete snapshot to carry the Provider id, got %+v", pending[0].WriteBackDelete)
	}

	if err := svc.SendWriteBack(ctx, pending[0]); err != nil {
		t.Fatalf("send write-back: %v", err)
	}
	if len(google.deleteRequests) != 1 {
		t.Fatalf("expected exactly one events.delete, got %d", len(google.deleteRequests))
	}
	if google.deleteRequests[0].EventID != "google-evt-1" || google.deleteRequests[0].CalendarID != "primary" {
		t.Fatalf("expected the delete addressed at primary/google-evt-1, got %+v", google.deleteRequests[0])
	}
}

// TestConnectionService_FullRefresh_NeverTombstonesALocallyCreatedEventWithAPendingPush
// is #292's ADR-0076 acceptance criterion: one locally created Event whose
// events.insert has not drained, meeting a Full Refresh over a listing that
// does not contain it, produces zero tombstones and the Event survives.
func TestConnectionService_FullRefresh_NeverTombstonesALocallyCreatedEventWithAPendingPush(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")},
	}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	// Created locally seconds ago — the push is still Pending, not drained,
	// and Google's listing (below) has never heard of it.
	created, err := g.Events.Create(ctx, userID, "evt-brand-new", EventWrite{
		CalendarID: calendar.ID, Title: "Just created", Start: time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 4, 1, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := svc.FullRefresh(ctx, userID, calendar.ID)
	if err != nil {
		t.Fatalf("full refresh: %v", err)
	}
	if result.Tombstoned != 0 {
		t.Fatalf("expected zero tombstones over a listing missing the just-created Event, got %+v", result)
	}

	if _, err := g.EventRepo.GetByID(ctx, created.ID); err != nil {
		t.Fatalf("expected the just-created Event to survive the Full Refresh, got %v", err)
	}
}

// TestConnectionService_DeltaRefresh_QueuedDeleteIsNotResurrectedByAnUpdate
// is #292's mirror acceptance criterion: a queued events.delete, meeting a
// Delta Refresh whose batch carries an ordinary update for that series, does
// not bring the Event back.
func TestConnectionService_DeltaRefresh_QueuedDeleteIsNotResurrectedByAnUpdate(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")},
	}
	google.nextSyncToken = "cursor-1"

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	if err := g.Events.Delete(ctx, userID, master.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// The DELETE push is Pending, not drained.

	// Meanwhile the Provider reports an ordinary update for the same series.
	google.eventsDeltaByToken = map[string][]map[string]any{
		"cursor-1": {googleEventItem("google-evt-1", "Standup (edited at provider)", "2026-01-01T10:00:00Z", "2026-01-01T10:30:00Z", "UTC")},
	}
	google.nextSyncToken = "cursor-2"

	result, err := svc.RefreshLinked(ctx, userID, calendar.ID, RefreshModeDelta)
	if err != nil {
		t.Fatalf("delta refresh: %v", err)
	}
	if result.Created != 0 || result.Updated != 0 {
		t.Fatalf("expected the queued-delete series to be left out of the batch entirely, got %+v", result)
	}
	if _, err := g.EventRepo.GetByID(ctx, master.ID); err != repository.ErrNotFound {
		t.Fatalf("expected the deleted Event to stay deleted, got %v", err)
	}
}

// TestConnectionService_FullRefresh_QueuedDeleteDoesNotAffectAnotherCalendarsSameIdSeries
// covers the review's most serious finding: a Provider assigns one event id
// to every attendee's copy, so the pending-delete set must be scoped to the
// calendar the delete targets. A queued delete for event id X on calendar A
// must not drop — let alone tombstone — a live series X on calendar B.
func TestConnectionService_FullRefresh_QueuedDeleteDoesNotAffectAnotherCalendarsSameIdSeries(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{
		googleCalendarItem("cal-a", "someone@gmail.com", "#0b8043", "owner", true),
		googleCalendarItem("cal-b", "someone@gmail.com", "#0b8043", "owner", true),
	}
	shared := func() map[string]any {
		return googleEventItem("shared-evt", "Team meeting", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")
	}
	google.eventsByCalendar = map[string][]map[string]any{
		"cal-a": {shared()},
		"cal-b": {shared()},
	}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	connectionID := connectUser(t, svc, auth, userID)
	created, err := svc.ImportCalendars(ctx, userID, workspaceID, connectionID, []string{"cal-a", "cal-b"})
	if err != nil {
		t.Fatalf("import calendars: %v", err)
	}
	var calA, calB repository.Calendar
	for _, c := range created {
		if c.Source != nil && *c.Source.ExternalCalendarID == "cal-a" {
			calA = c
		} else {
			calB = c
		}
	}

	mastersA, _, err := svc.events.ListSeriesByCalendar(ctx, userID, calA.ID)
	if err != nil {
		t.Fatalf("list series a: %v", err)
	}
	if err := g.Events.Delete(ctx, userID, mastersA[0].ID); err != nil {
		t.Fatalf("delete from cal-a: %v", err)
	}
	// The DELETE for "shared-evt" is Pending, not drained.

	result, err := svc.FullRefresh(ctx, userID, calB.ID)
	if err != nil {
		t.Fatalf("full refresh cal-b: %v", err)
	}
	if result.Tombstoned != 0 {
		t.Fatalf("expected cal-b's own copy untouched by cal-a's queued delete, got %+v", result)
	}
	mastersB, _, err := svc.events.ListSeriesByCalendar(ctx, userID, calB.ID)
	if err != nil {
		t.Fatalf("list series b: %v", err)
	}
	if len(mastersB) != 1 {
		t.Fatalf("expected cal-b to still hold its shared-evt series, got %d", len(mastersB))
	}
}

// TestConnectionService_SendWriteBack_DeleteRaisesNeedsAttentionWhenNoLongerWritable
// covers the review's finding: a queued events.delete that can't be
// delivered because the Source flipped read-only must surface — not silently
// no-op and let the next Refresh resurrect the Event.
func TestConnectionService_SendWriteBack_DeleteRaisesNeedsAttentionWhenNoLongerWritable(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")},
	}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	if err := g.Events.Delete(ctx, userID, master.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	pending := pendingWriteBacks(t, g, master.ID)

	// An ACL change flipped the Source read-only before the delete drained.
	if _, err := g.DB.ExecContext(ctx, `UPDATE calendar_sources SET mode = ? WHERE calendar_id = ?`, string(repository.SourceModeReadOnly), calendar.ID); err != nil {
		t.Fatalf("flip source read-only: %v", err)
	}

	// Skipped, not sent: the delete never reached Google, and the Source's
	// needs-attention state below is what actually reaches the User (ADR-0079).
	if err := svc.SendWriteBack(ctx, pending[0]); !errors.Is(err, outbox.ErrSkipped) {
		t.Fatalf("expected the undeliverable delete skipped, got %v", err)
	}
	if len(google.deleteRequests) != 0 {
		t.Fatalf("expected no delete sent to a read-only Source, got %d", len(google.deleteRequests))
	}
	src, err := g.SourceRepo.GetByCalendarID(ctx, calendar.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if src.ErrorClass == nil || *src.ErrorClass != ErrorClassNeedsAttention {
		t.Fatalf("expected the Source raised into needs-attention, got %v", src.ErrorClass)
	}
}

// TestConnectionService_Refresh_PermanentlyFailedDeleteRejoinsReconciliation
// covers #292's last bullet from the delete side: once a queued
// events.delete is permanently Failed, its ExternalUID drops out of the
// protection set, so a Refresh still listing the series at the Provider
// re-creates it — the Event "rejoins ordinary reconciliation". (A permanently
// failed *create* push instead leaves a local-only Event carrying a
// write-back-error marker: with no ExternalUID it is invisible to the
// reconciler, and tombstoning freshly authored content Google merely
// rejected would be worse than surfacing the marker.)
func TestConnectionService_Refresh_PermanentlyFailedDeleteRejoinsReconciliation(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("google-evt-1", "Standup", "2026-01-01T09:00:00Z", "2026-01-01T09:30:00Z", "UTC")},
	}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	if err := g.Events.Delete(ctx, userID, master.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	pending := pendingWriteBacks(t, g, master.ID)
	if len(pending) != 1 {
		t.Fatalf("expected one pending write-back, got %d", len(pending))
	}
	if err := g.OutboxRepo.MarkFailed(ctx, pending[0].ID, 5, "google keeps saying no"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	// Google still lists google-evt-1 — the delete never reached it.
	result, err := svc.FullRefresh(ctx, userID, calendar.ID)
	if err != nil {
		t.Fatalf("full refresh: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("expected the no-longer-protected series re-created from the Provider, got %+v", result)
	}
}
