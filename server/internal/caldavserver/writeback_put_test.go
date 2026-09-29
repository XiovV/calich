package caldavserver

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/icalendar"
	"github.com/XiovV/calich/server/internal/repository"
)

// This file covers the CalDAV surface of #299/ADR-0081: a PUT (or DELETE)
// reaching a writable Linked Calendar's Provider only once Exposure has made
// the collection visible at all, over real HTTP — the diff itself is
// event_put_series_writeback_test.go's, at the EventService seam this
// package's Backend calls straight into.

// putSeriesOnCalendar PUTs master (with no Overrides) at calendarID and
// returns the raw response, mirroring putSeriesStatus but keeping the whole
// *http.Response so a caller can also inspect headers.
func putSeriesOnCalendar(t *testing.T, env testCalDAVEnv, calendarID string, master repository.Event) *http.Response {
	t.Helper()
	return putSeriesWithOverrides(t, env, calendarID, master, nil)
}

// putSeriesWithOverrides is putSeriesOnCalendar with Overrides — master's own
// Rrule/Exdates fields carry the RRULE/EXDATE lines, and each override's
// RecurrenceID anchors it to the Occurrence it replaces (ADR-0016), exactly
// as a real client's whole-object PUT would encode them.
func putSeriesWithOverrides(t *testing.T, env testCalDAVEnv, calendarID string, master repository.Event, overrides []repository.Event) *http.Response {
	t.Helper()
	cal, _, err := icalendar.SeriesToICal(master, overrides, icalendar.CalDAVTarget(attachmentsBasePath))
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	return rawPut(t, env, calendarObjectPath(env.userID, calendarID, master.ID), cal, "")
}

func newTestPutMaster(id string) repository.Event {
	return repository.Event{
		ID:        id,
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
}

func pendingWriteBackRows(t *testing.T, env testCalDAVEnv) []repository.OutboxMessage {
	t.Helper()
	rows, err := repository.NewOutboxRepository(env.db).ListPending(context.Background(), 200)
	if err != nil {
		t.Fatalf("list pending outbox rows: %v", err)
	}
	var matched []repository.OutboxMessage
	for _, m := range rows {
		if m.Kind == repository.OutboxKindWriteBack {
			matched = append(matched, m)
		}
	}
	return matched
}

func acceptPutStatus(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 201 or 204, got %d", resp.StatusCode)
	}
}

// TestPutCalendarObject_ExposedWritableLinkedCalendar_QueuesCreateWriteBack
// covers #299's acceptance criterion that a create from a real CalDAV PUT
// reaches Google: once Exposure is on and the Source is writable, a PUT to a
// brand-new object queues an events.insert (POST) rather than answering 404
// the way it did before ADR-0081.
func TestPutCalendarObject_ExposedWritableLinkedCalendar_QueuesCreateWriteBack(t *testing.T) {
	env := newTestCalDAVEnv(t)
	linkedID := uuid.NewString()
	env.createLinkedCalendarWithMode(t, linkedID, "Linked", repository.SourceModeWritable)
	if err := env.calendarService.SetExposure(context.Background(), env.userID, linkedID, true); err != nil {
		t.Fatalf("expose linked calendar: %v", err)
	}

	master := newTestPutMaster("device-uid-create-1")
	resp := putSeriesOnCalendar(t, env, linkedID, master)
	defer resp.Body.Close()
	acceptPutStatus(t, resp)

	pending := pendingWriteBackRows(t, env)
	if len(pending) != 1 || pending[0].Method != repository.OutboxMethodPost {
		t.Fatalf("expected exactly one POST write-back, got %+v", pending)
	}
}

// TestPutCalendarObject_UnexposedWritableLinkedCalendar_404s covers
// ADR-0081's exposure gate directly over HTTP: a writable Linked Calendar
// the Owner hasn't turned Exposure on for isn't in their home-set to PUT
// into at all, so the object path 404s exactly like GetCalendar's own
// posture (ADR-0080) — and nothing is queued.
func TestPutCalendarObject_UnexposedWritableLinkedCalendar_404s(t *testing.T) {
	env := newTestCalDAVEnv(t)
	linkedID := uuid.NewString()
	env.createLinkedCalendarWithMode(t, linkedID, "Linked", repository.SourceModeWritable)
	// Deliberately left unexposed — the Owner's own default (ADR-0080).

	master := newTestPutMaster("device-uid-unexposed-1")
	resp := putSeriesOnCalendar(t, env, linkedID, master)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	if pending := pendingWriteBackRows(t, env); len(pending) != 0 {
		t.Fatalf("expected nothing queued, got %+v", pending)
	}
}

// TestPutCalendarObject_ReadOnlyLinkedCalendar_403sEvenWhenExposed covers
// that Exposure only makes a Linked Calendar's collection visible — it
// never overrides the Source's own read-only Mode (ADR-0075/ADR-0080's own
// "Access resolves first, Exposure second"). Once exposed, a read-only
// Source's object is visible enough to 403 rather than 404, matching a
// Subscribed Calendar's own posture.
func TestPutCalendarObject_ReadOnlyLinkedCalendar_403sEvenWhenExposed(t *testing.T) {
	env := newTestCalDAVEnv(t)
	linkedID := uuid.NewString()
	env.createLinkedCalendarWithMode(t, linkedID, "Linked", repository.SourceModeReadOnly)
	if err := env.calendarService.SetExposure(context.Background(), env.userID, linkedID, true); err != nil {
		t.Fatalf("expose linked calendar: %v", err)
	}

	master := newTestPutMaster("device-uid-readonly-1")
	resp := putSeriesOnCalendar(t, env, linkedID, master)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	if pending := pendingWriteBackRows(t, env); len(pending) != 0 {
		t.Fatalf("expected nothing queued, got %+v", pending)
	}
}

// TestDeleteCalendarObject_ExposedWritableLinkedCalendar_QueuesDeleteWriteBack
// covers #299's acceptance criterion that a delete from a real CalDAV DELETE
// reaches Google: EventService.Delete already builds the events.delete push
// for a writable Linked Calendar's Master (ADR-0077); this asserts the
// object path actually reaches it once Exposure says the collection is
// visible.
func TestDeleteCalendarObject_ExposedWritableLinkedCalendar_QueuesDeleteWriteBack(t *testing.T) {
	env := newTestCalDAVEnv(t)
	linkedID := uuid.NewString()
	env.createLinkedCalendarWithMode(t, linkedID, "Linked", repository.SourceModeWritable)
	if err := env.calendarService.SetExposure(context.Background(), env.userID, linkedID, true); err != nil {
		t.Fatalf("expose linked calendar: %v", err)
	}

	master := newTestPutMaster("device-uid-delete-1")
	putResp := putSeriesOnCalendar(t, env, linkedID, master)
	putResp.Body.Close()
	acceptPutStatus(t, putResp)

	delResp := rawDelete(t, env, calendarObjectPath(env.userID, linkedID, master.ID), "")
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", delResp.StatusCode)
	}

	var deleteRows int
	for _, m := range pendingWriteBackRows(t, env) {
		if m.Method == repository.OutboxMethodDelete {
			deleteRows++
		}
	}
	if deleteRows != 1 {
		t.Fatalf("expected exactly one DELETE write-back, got %d", deleteRows)
	}
}

// TestDeleteCalendarObject_UnexposedWritableLinkedCalendar_404s mirrors
// TestPutCalendarObject_UnexposedWritableLinkedCalendar_404s for DELETE.
func TestDeleteCalendarObject_UnexposedWritableLinkedCalendar_404s(t *testing.T) {
	env := newTestCalDAVEnv(t)
	linkedID := uuid.NewString()
	env.createLinkedCalendarWithMode(t, linkedID, "Linked", repository.SourceModeWritable)

	resp := rawDelete(t, env, calendarObjectPath(env.userID, linkedID, "device-uid-missing"), "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// soleWriteBackByMethod returns env's one pending write-back row of method,
// failing the test if there isn't exactly one — the real-HTTP counterpart to
// event_put_series_writeback_test.go's soleInstanceWriteBack, for asserting
// which push a real CalDAV PUT queued rather than just that one was queued.
func soleWriteBackByMethod(t *testing.T, env testCalDAVEnv, method string) repository.OutboxMessage {
	t.Helper()
	var matched []repository.OutboxMessage
	for _, m := range pendingWriteBackRows(t, env) {
		if m.Method == method {
			matched = append(matched, m)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("expected exactly one %s write-back, got %d: %+v", method, len(matched), matched)
	}
	return matched[0]
}

// TestPutCalendarObject_EditedMaster_QueuesPatchWriteBack covers the "Master
// fields changed" row of #299's delta table over real HTTP: a PUT to an
// object that already carries an ExternalUID (as one whose earlier
// events.insert already landed at Google would) queues a PATCH rather than
// another POST — this package deliberately wires no fake Google server in
// (ADR-0081's own "Seams" note), so the create having "already landed" is
// simulated the same way event_put_series_writeback_test.go's
// seedLinkedMaster fixture does, via a direct repository write.
func TestPutCalendarObject_EditedMaster_QueuesPatchWriteBack(t *testing.T) {
	env := newTestCalDAVEnv(t)
	linkedID := uuid.NewString()
	env.createLinkedCalendarWithMode(t, linkedID, "Linked", repository.SourceModeWritable)
	if err := env.calendarService.SetExposure(context.Background(), env.userID, linkedID, true); err != nil {
		t.Fatalf("expose linked calendar: %v", err)
	}

	master := newTestPutMaster("device-uid-edit-1")
	createResp := putSeriesOnCalendar(t, env, linkedID, master)
	createResp.Body.Close()
	acceptPutStatus(t, createResp)

	etag := "etag-already-linked"
	if err := repository.NewEventRepository(env.db).AdoptProviderIdentity(context.Background(), master.ID, "google-event-1", &etag); err != nil {
		t.Fatalf("adopt provider identity: %v", err)
	}
	// The create push above is otherwise still pending — its own dedup would
	// reuse that row unchanged rather than let the edit below queue a fresh
	// PATCH (harmless in practice, since SendWriteBack re-derives create-vs-
	// edit from the Event's own live ExternalUID rather than the row's
	// stored Method — but this test wants to name the row's method
	// explicitly). Mark it sent, as if it had actually landed.
	if _, err := env.db.Exec(`UPDATE outbox SET status = 'sent' WHERE event_id = ? AND kind = 'writeback'`, master.ID); err != nil {
		t.Fatalf("mark create push sent: %v", err)
	}

	master.Title = "Standup (renamed from a phone)"
	editResp := putSeriesOnCalendar(t, env, linkedID, master)
	defer editResp.Body.Close()
	acceptPutStatus(t, editResp)

	soleWriteBackByMethod(t, env, repository.OutboxMethodPatch)
}

// TestPutCalendarObject_AddsOverride_QueuesInstanceWriteBack covers the
// "Override added" row of #299's delta table over real HTTP: a PUT adding a
// recurring series' first Override queues an INSTANCE push naming that
// Occurrence's RECURRENCE-ID.
func TestPutCalendarObject_AddsOverride_QueuesInstanceWriteBack(t *testing.T) {
	env := newTestCalDAVEnv(t)
	linkedID := uuid.NewString()
	env.createLinkedCalendarWithMode(t, linkedID, "Linked", repository.SourceModeWritable)
	if err := env.calendarService.SetExposure(context.Background(), env.userID, linkedID, true); err != nil {
		t.Fatalf("expose linked calendar: %v", err)
	}

	master := newTestPutMaster("device-uid-override-add-1")
	master.Rrule = "FREQ=WEEKLY"
	createResp := putSeriesOnCalendar(t, env, linkedID, master)
	createResp.Body.Close()
	acceptPutStatus(t, createResp)

	recurrenceID := master.Start.AddDate(0, 0, 7)
	override := repository.Event{
		Title:        "Standup (moved)",
		Start:        recurrenceID.Add(time.Hour),
		End:          recurrenceID.Add(90 * time.Minute),
		RecurrenceID: &recurrenceID,
	}
	editResp := putSeriesWithOverrides(t, env, linkedID, master, []repository.Event{override})
	defer editResp.Body.Close()
	acceptPutStatus(t, editResp)

	msg := soleWriteBackByMethod(t, env, repository.OutboxMethodInstance)
	if !msg.WriteBackInstance.RecurrenceID.Equal(recurrenceID) {
		t.Fatalf("expected recurrence id %v, got %v", recurrenceID, msg.WriteBackInstance.RecurrenceID)
	}
}

// TestPutCalendarObject_AddsExdate_QueuesCancelInstanceWriteBack covers the
// "EXDATE added" row of #299's delta table over real HTTP: a PUT adding one
// EXDATE to a recurring series — "delete this event" on an ordinary
// Occurrence, the shape a real client encodes that scope as — queues a
// CANCEL_INSTANCE push naming it.
func TestPutCalendarObject_AddsExdate_QueuesCancelInstanceWriteBack(t *testing.T) {
	env := newTestCalDAVEnv(t)
	linkedID := uuid.NewString()
	env.createLinkedCalendarWithMode(t, linkedID, "Linked", repository.SourceModeWritable)
	if err := env.calendarService.SetExposure(context.Background(), env.userID, linkedID, true); err != nil {
		t.Fatalf("expose linked calendar: %v", err)
	}

	master := newTestPutMaster("device-uid-exdate-add-1")
	master.Rrule = "FREQ=WEEKLY"
	createResp := putSeriesOnCalendar(t, env, linkedID, master)
	createResp.Body.Close()
	acceptPutStatus(t, createResp)

	exdate := master.Start.AddDate(0, 0, 7)
	master.Exdates = []time.Time{exdate}
	editResp := putSeriesOnCalendar(t, env, linkedID, master)
	defer editResp.Body.Close()
	acceptPutStatus(t, editResp)

	msg := soleWriteBackByMethod(t, env, repository.OutboxMethodCancelInstance)
	if !msg.WriteBackInstance.RecurrenceID.Equal(exdate) {
		t.Fatalf("expected recurrence id %v, got %v", exdate, msg.WriteBackInstance.RecurrenceID)
	}
}

// TestPutCalendarObject_RevertsOverride_403sAndLeavesSeriesUnchanged covers
// the "Override removed (Occurrence reverts to the rule)" row of #299's
// delta table over real HTTP: a PUT that drops an existing Override with no
// matching new Exdate — asking to revert that Occurrence to the rule, which
// this app cannot yet push to Google — 403s, and the stored Override
// survives untouched (ADR-0081's "refused before any local write").
func TestPutCalendarObject_RevertsOverride_403sAndLeavesSeriesUnchanged(t *testing.T) {
	env := newTestCalDAVEnv(t)
	linkedID := uuid.NewString()
	env.createLinkedCalendarWithMode(t, linkedID, "Linked", repository.SourceModeWritable)
	if err := env.calendarService.SetExposure(context.Background(), env.userID, linkedID, true); err != nil {
		t.Fatalf("expose linked calendar: %v", err)
	}

	master := newTestPutMaster("device-uid-revert-1")
	master.Rrule = "FREQ=WEEKLY"
	recurrenceID := master.Start.AddDate(0, 0, 7)
	override := repository.Event{
		Title:        "Standup (moved)",
		Start:        recurrenceID.Add(time.Hour),
		End:          recurrenceID.Add(90 * time.Minute),
		RecurrenceID: &recurrenceID,
	}
	createResp := putSeriesWithOverrides(t, env, linkedID, master, []repository.Event{override})
	createResp.Body.Close()
	acceptPutStatus(t, createResp)

	// Resend with the Override simply gone and no matching Exdate — a
	// revert, not a delete.
	revertResp := putSeriesOnCalendar(t, env, linkedID, master)
	defer revertResp.Body.Close()
	if revertResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", revertResp.StatusCode)
	}

	_, overrides, err := env.eventService.GetSeries(context.Background(), env.userID, master.ID)
	if err != nil {
		t.Fatalf("get series after refusal: %v", err)
	}
	if len(overrides) != 1 {
		t.Fatalf("expected the refused PUT to leave the stored override in place, got %+v", overrides)
	}
}
