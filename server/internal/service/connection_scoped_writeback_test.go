package service

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// This file drives the three recurring-edit scopes' Provider push end to end
// (#293, ADR-0078) — SendWriteBack against a real local fake-Google server:
// the instance id is resolved from events.instances by original start (never
// constructed), a cancel is a status:cancelled instance (never an EXDATE), and
// the "this and following" split is performed by hand.

// recurringSeriesFixture is one weekly Google series a scoped-edit test
// imports and then edits locally. tz "" makes it all-day (start is a date).
func recurringSeriesFixture(id, summary, start, end, tz string, rrule string) map[string]any {
	item := map[string]any{
		"id":         id,
		"etag":       `"etag-` + id + `"`,
		"summary":    summary,
		"recurrence": []string{rrule},
	}
	if tz == "" {
		item["start"] = map[string]any{"date": start}
		item["end"] = map[string]any{"date": end}
	} else {
		item["start"] = map[string]any{"dateTime": start, "timeZone": tz}
		item["end"] = map[string]any{"dateTime": end, "timeZone": tz}
	}
	return item
}

func drainWriteBacks(t *testing.T, svc *ConnectionService, g *Graph, masterID string) {
	t.Helper()
	for _, msg := range pendingWriteBacks(t, g, masterID) {
		if err := svc.SendWriteBack(context.Background(), msg); err != nil {
			t.Fatalf("send write-back %s: %v", msg.Method, err)
		}
	}
}

func TestConnectionService_SendWriteBackInstance_ResolvesInstanceIdAndPatchesIt(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {recurringSeriesFixture("series-1", "Standup", "2026-01-05T09:00:00-05:00", "2026-01-05T09:15:00-05:00", "America/New_York", "RRULE:FREQ=WEEKLY")},
	}
	google.instancesByOriginalStart = map[string]map[string]any{
		"2026-01-12T09:00:00-05:00": {"id": "series-1_20260112T140000Z", "etag": `"instance-etag-1"`, "recurringEventId": "series-1", "status": "confirmed"},
	}
	google.patchResponseETag = "instance-etag-after-push"

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	recurrenceID := master.Start.AddDate(0, 0, 7) // 2026-01-12 09:00 EST
	override, err := g.Events.Create(ctx, userID, "evt-override-1", EventWrite{
		CalendarID: calendar.ID, Title: "Standup (this week only)",
		Start: recurrenceID.Add(time.Hour), End: recurrenceID.Add(90 * time.Minute),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	drainWriteBacks(t, svc, g, master.ID)

	if len(google.instancesRequests) != 1 {
		t.Fatalf("expected exactly one events.instances lookup, got %d", len(google.instancesRequests))
	}
	if got := google.instancesRequests[0].OriginalStart; got != "2026-01-12T09:00:00-05:00" {
		t.Fatalf("expected the instance resolved by its original start, got %q", got)
	}
	if google.instancesRequests[0].EventID != "series-1" {
		t.Fatalf("expected events.instances on the series master, got %q", google.instancesRequests[0].EventID)
	}
	if len(google.patchRequests) != 1 {
		t.Fatalf("expected exactly one PATCH, got %d", len(google.patchRequests))
	}
	push := google.patchRequests[0]
	if push.EventID != "series-1_20260112T140000Z" {
		t.Fatalf("expected the PATCH addressed at the resolved instance id, got %q", push.EventID)
	}
	if push.IfMatch != `"instance-etag-1"` {
		t.Fatalf("expected If-Match carrying the instance's own etag, got %q", push.IfMatch)
	}
	if push.Body["summary"] != "Standup (this week only)" {
		t.Fatalf("expected the override title in the push, got %v", push.Body["summary"])
	}
	if _, hasRecurrence := push.Body["recurrence"]; hasRecurrence {
		t.Fatalf("an instance PATCH must carry no recurrence, got %v", push.Body["recurrence"])
	}

	got, err := g.EventRepo.GetByID(ctx, override.ID)
	if err != nil {
		t.Fatalf("get override: %v", err)
	}
	if got.ProviderEtag == nil || *got.ProviderEtag != "instance-etag-after-push" {
		t.Fatalf("expected the push response's etag echoed onto the override, got %v", got.ProviderEtag)
	}
}

func TestConnectionService_SendWriteBackInstance_AllDayOriginalStartIsABareDate(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {recurringSeriesFixture("bday-1", "Birthday", "2026-02-02", "2026-02-03", "", "RRULE:FREQ=WEEKLY")},
	}
	google.instancesByOriginalStart = map[string]map[string]any{
		"2026-02-09": {"id": "bday-1_20260209", "etag": `"i2"`, "recurringEventId": "bday-1", "status": "confirmed"},
	}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)
	if !master.AllDay {
		t.Fatalf("expected an all-day master, got %+v", master)
	}

	recurrenceID := master.Start.AddDate(0, 0, 7) // 2026-02-09
	if _, err := g.Events.Create(ctx, userID, "evt-allday-override", EventWrite{
		CalendarID: calendar.ID, Title: "Birthday (party this week)", AllDay: true,
		Start: recurrenceID, End: recurrenceID.AddDate(0, 0, 1),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	}); err != nil {
		t.Fatalf("create all-day override: %v", err)
	}
	drainWriteBacks(t, svc, g, master.ID)

	if len(google.instancesRequests) != 1 || google.instancesRequests[0].OriginalStart != "2026-02-09" {
		t.Fatalf("expected the all-day instance resolved by a bare date, got %+v", google.instancesRequests)
	}
	if len(google.patchRequests) != 1 || google.patchRequests[0].EventID != "bday-1_20260209" {
		t.Fatalf("expected a PATCH on the resolved all-day instance, got %+v", google.patchRequests)
	}
}

func TestConnectionService_SendWriteBackInstance_DSTOriginalStartCarriesTheDaylightOffset(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {recurringSeriesFixture("dst-1", "Standup", "2026-03-02T09:00:00-05:00", "2026-03-02T09:15:00-05:00", "America/New_York", "RRULE:FREQ=WEEKLY")},
	}
	google.instancesByOriginalStart = map[string]map[string]any{
		"2026-03-16T09:00:00-04:00": {"id": "dst-1_20260316T130000Z", "etag": `"i3"`, "recurringEventId": "dst-1", "status": "confirmed"},
	}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	// The Monday after DST began (2026-03-08): 09:00 America/New_York is
	// 13:00Z, not the 14:00Z a naive +14d on the master's stored instant would
	// give. formatGoogleOriginalStart must render it -04:00, not -05:00.
	recurrenceID := time.Date(2026, 3, 16, 13, 0, 0, 0, time.UTC)
	if _, err := g.Events.Create(ctx, userID, "evt-dst-override", EventWrite{
		CalendarID: calendar.ID, Title: "Standup (DST week)",
		Start: recurrenceID, End: recurrenceID.Add(15 * time.Minute),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	}); err != nil {
		t.Fatalf("create DST override: %v", err)
	}
	drainWriteBacks(t, svc, g, master.ID)

	if len(google.instancesRequests) != 1 || google.instancesRequests[0].OriginalStart != "2026-03-16T09:00:00-04:00" {
		t.Fatalf("expected the DST instance resolved with the daylight offset, got %+v", google.instancesRequests)
	}
	if len(google.patchRequests) != 1 || google.patchRequests[0].EventID != "dst-1_20260316T130000Z" {
		t.Fatalf("expected a PATCH on the resolved DST instance, got %+v", google.patchRequests)
	}
}

func TestConnectionService_SendWriteBackInstanceCancel_PatchesStatusCancelledNeverAnExdate(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {recurringSeriesFixture("series-c", "Standup", "2026-01-05T09:00:00-05:00", "2026-01-05T09:15:00-05:00", "America/New_York", "RRULE:FREQ=WEEKLY")},
	}
	google.instancesByOriginalStart = map[string]map[string]any{
		"2026-01-19T09:00:00-05:00": {"id": "series-c_20260119", "etag": `"ic"`, "recurringEventId": "series-c", "status": "confirmed"},
	}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	recurrenceID := master.Start.AddDate(0, 0, 14) // 2026-01-19 09:00 EST
	if err := g.Events.AddException(ctx, userID, master.ID, recurrenceID); err != nil {
		t.Fatalf("add exception: %v", err)
	}
	drainWriteBacks(t, svc, g, master.ID)

	if len(google.patchRequests) != 1 {
		t.Fatalf("expected exactly one PATCH, got %d", len(google.patchRequests))
	}
	push := google.patchRequests[0]
	if push.EventID != "series-c_20260119" {
		t.Fatalf("expected the cancel PATCH on the resolved instance, got %q", push.EventID)
	}
	if push.Body["status"] != "cancelled" {
		t.Fatalf("expected status:cancelled, got %v", push.Body["status"])
	}
	// A cancel carries no content fields at all — no title, no times, and
	// above all no recurrence array that could hold an EXDATE line. (The fake
	// server echoes an id/etag onto the captured body afterward; those are
	// response artifacts, not something the request sent.)
	for _, forbidden := range []string{"summary", "start", "end", "recurrence", "description", "location"} {
		if _, ok := push.Body[forbidden]; ok {
			t.Fatalf("a cancel PATCH must carry no %q, got %v", forbidden, push.Body)
		}
	}
}

// TestConnectionService_SendWriteBackInstanceCancel_AlreadyGoneAtGoogleIsSuccess
// covers the deleteEvent-parity finding: an occurrence hard-deleted at Google
// between the events.instances lookup and the cancel PATCH (a 404/410) is the
// end state the User asked for — success, not a permanent failure that raises
// the Source.
func TestConnectionService_SendWriteBackInstanceCancel_AlreadyGoneAtGoogleIsSuccess(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {recurringSeriesFixture("series-g", "Standup", "2026-01-05T09:00:00-05:00", "2026-01-05T09:15:00-05:00", "America/New_York", "RRULE:FREQ=WEEKLY")},
	}
	google.instancesByOriginalStart = map[string]map[string]any{
		"2026-01-19T09:00:00-05:00": {"id": "series-g_20260119", "etag": `"ig"`, "recurringEventId": "series-g", "status": "confirmed"},
	}
	google.patchStatus = 404 // the instance is gone by the time the cancel PATCH lands

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	recurrenceID := master.Start.AddDate(0, 0, 14)
	if err := g.Events.AddException(ctx, userID, master.ID, recurrenceID); err != nil {
		t.Fatalf("add exception: %v", err)
	}
	for _, msg := range pendingWriteBacks(t, g, master.ID) {
		if err := svc.SendWriteBack(ctx, msg); err != nil {
			t.Fatalf("expected an already-gone occurrence to be success, got %v", err)
		}
	}

	got, err := g.EventRepo.GetByID(ctx, master.ID)
	if err != nil {
		t.Fatalf("get master: %v", err)
	}
	if got.WriteBackError != nil {
		t.Fatalf("expected no permanent-failure marker, got %v", *got.WriteBackError)
	}
	src, err := g.SourceRepo.GetByCalendarID(ctx, calendar.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if src.ErrorClass != nil {
		t.Fatalf("expected the Source unraised, got %v", *src.ErrorClass)
	}
}

// TestConnectionService_SendWriteBack_MasterEditNeverSendsAnExdateLine covers
// AC3 at the send level: a series with a cancellation this app reconciled in
// from Google, edited "all events", must PATCH a recurrence array of exactly
// the RRULE — the cancellation stays a status:cancelled instance at Google.
func TestConnectionService_SendWriteBack_MasterEditNeverSendsAnExdateLine(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {
			recurringSeriesFixture("series-x", "Standup", "2026-01-05T09:00:00-05:00", "2026-01-05T09:15:00-05:00", "America/New_York", "RRULE:FREQ=WEEKLY"),
			{
				"id":                "series-x_20260119",
				"recurringEventId":  "series-x",
				"originalStartTime": map[string]any{"dateTime": "2026-01-19T09:00:00-05:00", "timeZone": "America/New_York"},
				"status":            "cancelled",
			},
		},
	}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	master := soleMasterOf(t, g, calendar.ID)

	if _, err := g.Events.Update(ctx, userID, master.ID, EventWrite{
		CalendarID: calendar.ID, Title: "Standup (renamed)", Start: master.Start, End: master.End, Rrule: master.Rrule,
	}); err != nil {
		t.Fatalf("update master: %v", err)
	}
	drainWriteBacks(t, svc, g, master.ID)

	if len(google.patchRequests) != 1 {
		t.Fatalf("expected one PATCH, got %d", len(google.patchRequests))
	}
	recurrence, ok := google.patchRequests[0].Body["recurrence"].([]any)
	if !ok || len(recurrence) != 1 {
		t.Fatalf("expected exactly the RRULE line, got %v", google.patchRequests[0].Body["recurrence"])
	}
	if recurrence[0] != "RRULE:FREQ=WEEKLY" {
		t.Fatalf("expected RRULE:FREQ=WEEKLY, got %v", recurrence[0])
	}
}

// TestConnectionService_ThisAndFollowing_SplitsByHand covers AC4: the three
// calls the frontend dispatches for a "this and following" edit
// (update-the-old, create-the-new, reparent) reach Google as an UNTIL-bounded
// PATCH on the old series plus an events.insert for the new one whose returned
// id is adopted onto the re-anchored local Master.
func TestConnectionService_ThisAndFollowing_SplitsByHand(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {recurringSeriesFixture("series-s", "Standup", "2026-01-05T09:00:00-05:00", "2026-01-05T09:15:00-05:00", "America/New_York", "RRULE:FREQ=WEEKLY")},
	}
	google.insertResponseETag = "split-etag"

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	oldMaster := soleMasterOf(t, g, calendar.ID)
	splitStart := oldMaster.Start.AddDate(0, 0, 21)

	// 1. update the old series with an UNTIL bound.
	if _, err := g.Events.Update(ctx, userID, oldMaster.ID, EventWrite{
		CalendarID: calendar.ID, Title: oldMaster.Title, Start: oldMaster.Start, End: oldMaster.End,
		Rrule: "FREQ=WEEKLY;UNTIL=20260125T140000Z",
	}); err != nil {
		t.Fatalf("update old master: %v", err)
	}
	// 2. create the new series carrying the remainder.
	newMaster, err := g.Events.Create(ctx, userID, "series-s-new", EventWrite{
		CalendarID: calendar.ID, Title: "Standup (revised)", Start: splitStart, End: splitStart.Add(15 * time.Minute),
		Rrule: "FREQ=WEEKLY",
	})
	if err != nil {
		t.Fatalf("create new master: %v", err)
	}
	// 3. reparent overrides/exceptions at the boundary (no rows here — no-op).
	if err := g.Events.ReparentFrom(ctx, userID, oldMaster.ID, newMaster.ID, splitStart); err != nil {
		t.Fatalf("reparent: %v", err)
	}

	drainWriteBacks(t, svc, g, oldMaster.ID)
	drainWriteBacks(t, svc, g, newMaster.ID)

	if len(google.patchRequests) != 1 || google.patchRequests[0].EventID != "series-s" {
		t.Fatalf("expected one PATCH bounding the old series, got %+v", google.patchRequests)
	}
	if rec, _ := google.patchRequests[0].Body["recurrence"].([]any); len(rec) != 1 || rec[0] != "RRULE:FREQ=WEEKLY;UNTIL=20260125T140000Z" {
		t.Fatalf("expected the UNTIL-bounded rule pushed, got %v", google.patchRequests[0].Body["recurrence"])
	}
	if len(google.insertRequests) != 1 {
		t.Fatalf("expected one events.insert for the new series, got %d", len(google.insertRequests))
	}

	got, err := g.EventRepo.GetByID(ctx, newMaster.ID)
	if err != nil {
		t.Fatalf("get new master: %v", err)
	}
	// The insert supplies a deterministic client id (#292's idempotency), and
	// the fake Google honours it — so the adopted Provider id is that id, and
	// it is what the re-anchored local series now carries.
	wantID := googleClientEventID(newMaster.ID)
	if got.ExternalUID == nil || *got.ExternalUID != wantID {
		t.Fatalf("expected the new master to adopt its Provider id %q, got %v", wantID, got.ExternalUID)
	}
	if got.ExternalUID != nil && google.insertRequests[0].Body["id"] != *got.ExternalUID {
		t.Fatalf("expected the insert to carry the same id that was adopted, got %v vs %v", google.insertRequests[0].Body["id"], *got.ExternalUID)
	}
}

// TestConnectionService_ThisAndFollowing_HalfSplitIsDetectableAndRecoverable
// covers AC7: the new series' events.insert failing terminally leaves the new
// Master carrying the write-back-error marker and its Source in
// needs-attention (detectable), and a later edit re-enqueues the insert and
// clears the marker (recoverable).
func TestConnectionService_ThisAndFollowing_HalfSplitIsDetectableAndRecoverable(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {recurringSeriesFixture("series-h", "Standup", "2026-01-05T09:00:00-05:00", "2026-01-05T09:15:00-05:00", "America/New_York", "RRULE:FREQ=WEEKLY")},
	}
	google.insertStatus = 403 // the new series can't be created at Google

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	oldMaster := soleMasterOf(t, g, calendar.ID)
	splitStart := oldMaster.Start.AddDate(0, 0, 21)

	newMaster, err := g.Events.Create(ctx, userID, "series-h-new", EventWrite{
		CalendarID: calendar.ID, Title: "Standup (revised)", Start: splitStart, End: splitStart.Add(15 * time.Minute),
		Rrule: "FREQ=WEEKLY",
	})
	if err != nil {
		t.Fatalf("create new master: %v", err)
	}

	pending := pendingWriteBacks(t, g, newMaster.ID)
	if len(pending) != 1 {
		t.Fatalf("expected one pending insert, got %d", len(pending))
	}
	// The insert fails; the outbox eventually exhausts its backoff and the
	// Worker calls the terminal hook.
	dispatcher := &OutboxDispatcher{Mail: NewInvitationSender(g.Events, nil, "calich@example.com"), WriteBack: svc}
	if err := svc.SendWriteBack(ctx, pending[0]); err == nil {
		t.Fatalf("expected the insert to fail with 403")
	}
	if err := dispatcher.HandleTerminalFailure(ctx, pending[0], errGoogleInstanceNotFound); err != nil {
		t.Fatalf("handle terminal failure: %v", err)
	}

	half, err := g.EventRepo.GetByID(ctx, newMaster.ID)
	if err != nil {
		t.Fatalf("get new master: %v", err)
	}
	if half.WriteBackError == nil {
		t.Fatalf("expected the half-split new master to carry a write-back-error marker")
	}
	src, err := g.SourceRepo.GetByCalendarID(ctx, calendar.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if src.ErrorClass == nil || *src.ErrorClass != ErrorClassNeedsAttention {
		t.Fatalf("expected the Source raised into needs-attention, got %v", src.ErrorClass)
	}

	// Recovery: Google accepts the insert now, and a fresh edit re-enqueues it.
	google.insertStatus = 0
	if _, err := g.Events.Update(ctx, userID, newMaster.ID, EventWrite{
		CalendarID: calendar.ID, Title: "Standup (revised again)", Start: newMaster.Start, End: newMaster.End, Rrule: "FREQ=WEEKLY",
	}); err != nil {
		t.Fatalf("edit the half-split master: %v", err)
	}
	recovered, err := g.EventRepo.GetByID(ctx, newMaster.ID)
	if err != nil {
		t.Fatalf("get new master: %v", err)
	}
	if recovered.WriteBackError != nil {
		t.Fatalf("expected the marker cleared by the fresh edit, got %v", *recovered.WriteBackError)
	}
	retry := pendingWriteBacks(t, g, newMaster.ID)
	if len(retry) != 1 || retry[0].Method != repository.OutboxMethodPost {
		t.Fatalf("expected the fresh edit to re-enqueue the insert, got %+v", retry)
	}
	drainWriteBacks(t, svc, g, newMaster.ID)
	if len(google.insertRequests) < 1 {
		t.Fatalf("expected the retried insert to reach Google")
	}
}

// TestConnectionService_FullRefresh_NeverRevertsAnOccurrenceWithAPendingInstancePush
// is the ADR-0076 protection extended to a scoped edit: an Override whose
// instance PATCH is still queued must survive a Full Refresh that lists the
// series with its pre-edit content.
func TestConnectionService_FullRefresh_NeverRevertsAnOccurrenceWithAPendingInstancePush(t *testing.T) {
	ctx := context.Background()
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {
			recurringSeriesFixture("series-p", "Standup", "2026-01-05T09:00:00-05:00", "2026-01-05T09:15:00-05:00", "America/New_York", "RRULE:FREQ=WEEKLY"),
			{
				"id":                "series-p_20260112",
				"recurringEventId":  "series-p",
				"originalStartTime": map[string]any{"dateTime": "2026-01-12T09:00:00-05:00", "timeZone": "America/New_York"},
				"summary":           "Standup (google's copy)",
				"start":             map[string]any{"dateTime": "2026-01-12T09:00:00-05:00", "timeZone": "America/New_York"},
				"end":               map[string]any{"dateTime": "2026-01-12T09:15:00-05:00", "timeZone": "America/New_York"},
				"status":            "confirmed",
			},
		},
	}

	svc, g, auth, userID, workspaceID := newTestConnectionServiceForWriteBack(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	masters, err := g.EventRepo.ListMastersByCalendar(ctx, calendar.ID)
	if err != nil || len(masters) != 1 {
		t.Fatalf("expected exactly one imported master, got %d (%v)", len(masters), err)
	}
	master := masters[0]

	recurrenceID := master.Start.AddDate(0, 0, 7)
	override, err := g.Events.GetOverrideForWriteBack(ctx, master.ID, recurrenceID)
	if err != nil {
		t.Fatalf("load imported override: %v", err)
	}
	if _, err := g.Events.Update(ctx, userID, override.ID, EventWrite{
		CalendarID: calendar.ID, Title: "Standup (my local edit, not yet pushed)", Start: override.Start, End: override.End,
	}); err != nil {
		t.Fatalf("edit override: %v", err)
	}

	// Full Refresh runs before the instance PATCH drains; Google still answers
	// with its own pre-edit copy.
	if _, err := svc.FullRefresh(ctx, userID, calendar.ID); err != nil {
		t.Fatalf("full refresh: %v", err)
	}

	got, err := g.EventRepo.GetByID(ctx, override.ID)
	if err != nil {
		t.Fatalf("get override: %v", err)
	}
	if got.Title != "Standup (my local edit, not yet pushed)" {
		t.Fatalf("expected the local occurrence edit to survive the Refresh, got %q", got.Title)
	}
}
