package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// This file covers PutSeries' own compiler (#299, ADR-0081): a CalDAV PUT
// diffed against a writable Linked Calendar's stored state, turned into the
// right outbox rows (or refused outright, atomically) before anything is
// written. The seam matches event_scoped_writeback_test.go's own: which
// outbox row a write produces, and that a delta this app can't yet express
// refuses cleanly — not what SendWriteBack does with the row once queued.

// exposeLinkedCalendar turns Exposure on for userID's own Linked Calendar
// (ADR-0080) — every PutSeries test needs this, since a Linked Calendar's own
// Owner defaults to *unexposed*, and PutSeries answers repository.ErrNotFound
// for a PUT to a collection that isn't in the caller's home-set at all.
func exposeLinkedCalendar(t *testing.T, g *Graph, userID int64, calendarID string) {
	t.Helper()
	if err := g.Calendars.SetExposure(context.Background(), userID, calendarID, true); err != nil {
		t.Fatalf("expose linked calendar: %v", err)
	}
}

func basicSeriesWrite(title string, start, end time.Time) SeriesWrite {
	return SeriesWrite{Title: title, Start: start, End: end}
}

func TestEventService_PutSeries_NewSeries_EnqueuesCreate(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	exposeLinkedCalendar(t, g, userID, calendarID)

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	masterID := "put-new-master"
	_, _, err := g.Events.PutSeries(ctx, userID, calendarID, masterID, basicSeriesWrite("Standup", start, end))
	if err != nil {
		t.Fatalf("put series: %v", err)
	}

	pending := pendingWriteBacks(t, g, masterID)
	if len(pending) != 1 || pending[0].Method != repository.OutboxMethodPost {
		t.Fatalf("expected exactly one POST write-back, got %+v", pending)
	}
}

func TestEventService_PutSeries_MasterFieldsChanged_EnqueuesPatch(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	exposeLinkedCalendar(t, g, userID, calendarID)
	master := seedLinkedMaster(t, g, userID, calendarID, "put-patch-master")

	_, _, err := g.Events.PutSeries(ctx, userID, calendarID, master.ID, basicSeriesWrite("Standup (renamed)", master.Start, master.End))
	if err != nil {
		t.Fatalf("put series: %v", err)
	}

	pending := pendingWriteBacks(t, g, master.ID)
	if len(pending) != 1 || pending[0].Method != repository.OutboxMethodPatch {
		t.Fatalf("expected exactly one PATCH write-back, got %+v", pending)
	}
}

func TestEventService_PutSeries_MasterUnchanged_EnqueuesNothing(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	exposeLinkedCalendar(t, g, userID, calendarID)
	master := seedLinkedMaster(t, g, userID, calendarID, "put-noop-master")

	_, _, err := g.Events.PutSeries(ctx, userID, calendarID, master.ID, basicSeriesWrite(master.Title, master.Start, master.End))
	if err != nil {
		t.Fatalf("put series: %v", err)
	}

	if pending := pendingWriteBacks(t, g, master.ID); len(pending) != 0 {
		t.Fatalf("expected no write-back for an unchanged PUT, got %+v", pending)
	}
}

func TestEventService_PutSeries_OverrideAdded_EnqueuesInstance(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	exposeLinkedCalendar(t, g, userID, calendarID)
	master := seedLinkedRecurringMaster(t, g, userID, calendarID, "put-override-add-master")

	recurrenceID := master.Start.AddDate(0, 0, 7)
	write := basicSeriesWrite(master.Title, master.Start, master.End)
	write.Rrule = master.Rrule
	write.Overrides = []OverrideWrite{{
		RecurrenceID: recurrenceID,
		Title:        "Standup (moved)",
		Start:        recurrenceID.Add(time.Hour),
		End:          recurrenceID.Add(90 * time.Minute),
	}}

	_, overrides, err := g.Events.PutSeries(ctx, userID, calendarID, master.ID, write)
	if err != nil {
		t.Fatalf("put series: %v", err)
	}
	if len(overrides) != 1 {
		t.Fatalf("expected exactly one stored override, got %+v", overrides)
	}

	msg := soleInstanceWriteBack(t, g, master.ID, repository.OutboxMethodInstance)
	if msg.WriteBackInstance.OverrideEventID != overrides[0].ID {
		t.Fatalf("expected the instance push to name the new override %q, got %q", overrides[0].ID, msg.WriteBackInstance.OverrideEventID)
	}
	if !msg.WriteBackInstance.RecurrenceID.Equal(recurrenceID) {
		t.Fatalf("expected recurrence id %v, got %v", recurrenceID, msg.WriteBackInstance.RecurrenceID)
	}
}

func TestEventService_PutSeries_OverrideChanged_EnqueuesInstance(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	exposeLinkedCalendar(t, g, userID, calendarID)
	master := seedLinkedRecurringMaster(t, g, userID, calendarID, "put-override-change-master")
	recurrenceID := master.Start.AddDate(0, 0, 7)
	override := seedLinkedOverride(t, g, userID, calendarID, master.ID, "put-override-change-override", recurrenceID)

	write := basicSeriesWrite(master.Title, master.Start, master.End)
	write.Rrule = master.Rrule
	write.Overrides = []OverrideWrite{{
		RecurrenceID: recurrenceID,
		Title:        "Standup (retitled)",
		Start:        override.Start,
		End:          override.End,
	}}

	_, _, err := g.Events.PutSeries(ctx, userID, calendarID, master.ID, write)
	if err != nil {
		t.Fatalf("put series: %v", err)
	}

	msg := soleInstanceWriteBack(t, g, master.ID, repository.OutboxMethodInstance)
	if msg.WriteBackInstance.OverrideEventID != override.ID {
		t.Fatalf("expected the instance push to name the existing override %q, got %q", override.ID, msg.WriteBackInstance.OverrideEventID)
	}
}

func TestEventService_PutSeries_OverrideUnchanged_EnqueuesNothing(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	exposeLinkedCalendar(t, g, userID, calendarID)
	master := seedLinkedRecurringMaster(t, g, userID, calendarID, "put-override-noop-master")
	recurrenceID := master.Start.AddDate(0, 0, 7)
	override := seedLinkedOverride(t, g, userID, calendarID, master.ID, "put-override-noop-override", recurrenceID)

	write := basicSeriesWrite(master.Title, master.Start, master.End)
	write.Rrule = master.Rrule
	write.Overrides = []OverrideWrite{{
		RecurrenceID: recurrenceID,
		Title:        override.Title,
		Start:        override.Start,
		End:          override.End,
	}}

	_, _, err := g.Events.PutSeries(ctx, userID, calendarID, master.ID, write)
	if err != nil {
		t.Fatalf("put series: %v", err)
	}

	if pending := pendingWriteBacks(t, g, master.ID); len(pending) != 0 {
		t.Fatalf("expected no write-back for an unchanged override, got %+v", pending)
	}
}

func TestEventService_PutSeries_ExdateAdded_EnqueuesCancelInstance(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	exposeLinkedCalendar(t, g, userID, calendarID)
	master := seedLinkedRecurringMaster(t, g, userID, calendarID, "put-exdate-add-master")
	exdate := master.Start.AddDate(0, 0, 14)

	write := basicSeriesWrite(master.Title, master.Start, master.End)
	write.Rrule = master.Rrule
	write.Exdates = []time.Time{exdate}

	_, _, err := g.Events.PutSeries(ctx, userID, calendarID, master.ID, write)
	if err != nil {
		t.Fatalf("put series: %v", err)
	}

	msg := soleInstanceWriteBack(t, g, master.ID, repository.OutboxMethodCancelInstance)
	if msg.WriteBackInstance.OverrideEventID != "" {
		t.Fatalf("expected no override event id on a plain exdate cancel, got %q", msg.WriteBackInstance.OverrideEventID)
	}
	if !msg.WriteBackInstance.RecurrenceID.Equal(exdate) {
		t.Fatalf("expected recurrence id %v, got %v", exdate, msg.WriteBackInstance.RecurrenceID)
	}
}

func TestEventService_PutSeries_OverrideRemoved_RefusesAndLeavesSeriesUnchanged(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	exposeLinkedCalendar(t, g, userID, calendarID)
	master := seedLinkedRecurringMaster(t, g, userID, calendarID, "put-override-revert-master")
	recurrenceID := master.Start.AddDate(0, 0, 7)
	seedLinkedOverride(t, g, userID, calendarID, master.ID, "put-override-revert-override", recurrenceID)

	// Resend the series with no Overrides and no Exdates at all — "revert
	// this Occurrence to what the rule generates", which this app cannot
	// yet push to Google.
	write := basicSeriesWrite(master.Title, master.Start, master.End)
	write.Rrule = master.Rrule

	_, _, err := g.Events.PutSeries(ctx, userID, calendarID, master.ID, write)
	if !errors.Is(err, ErrLinkedCalendarWriteBackRevertUnsupported) {
		t.Fatalf("expected ErrLinkedCalendarWriteBackRevertUnsupported, got %v", err)
	}

	if pending := pendingWriteBacks(t, g, master.ID); len(pending) != 0 {
		t.Fatalf("expected the refusal to queue nothing, got %+v", pending)
	}
	_, overrides, err := g.Events.GetSeries(ctx, userID, master.ID)
	if err != nil {
		t.Fatalf("get series after refusal: %v", err)
	}
	if len(overrides) != 1 {
		t.Fatalf("expected the refused PUT to leave the stored override in place, got %+v", overrides)
	}
}

func TestEventService_PutSeries_ExdateRemoved_RefusesAndLeavesSeriesUnchanged(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	exposeLinkedCalendar(t, g, userID, calendarID)
	master := seedLinkedRecurringMaster(t, g, userID, calendarID, "put-exdate-revert-master")
	exdate := master.Start.AddDate(0, 0, 7)
	if err := g.EventExceptionRepo.Add(ctx, master.ID, exdate); err != nil {
		t.Fatalf("seed exdate: %v", err)
	}

	// Resend the series with the Exdate simply gone — "un-cancel this
	// Occurrence", also deferred.
	write := basicSeriesWrite(master.Title, master.Start, master.End)
	write.Rrule = master.Rrule

	_, _, err := g.Events.PutSeries(ctx, userID, calendarID, master.ID, write)
	if !errors.Is(err, ErrLinkedCalendarWriteBackRevertUnsupported) {
		t.Fatalf("expected ErrLinkedCalendarWriteBackRevertUnsupported, got %v", err)
	}

	if pending := pendingWriteBacks(t, g, master.ID); len(pending) != 0 {
		t.Fatalf("expected the refusal to queue nothing, got %+v", pending)
	}
	stored, err := g.EventExceptionRepo.ListByParentIDs(ctx, []string{master.ID})
	if err != nil {
		t.Fatalf("list exceptions after refusal: %v", err)
	}
	if len(stored[master.ID]) != 1 {
		t.Fatalf("expected the refused PUT to leave the stored exdate in place, got %+v", stored[master.ID])
	}
}

// TestEventService_PutSeries_OverrideRemovedWithMatchingExdate_CancelsInstance
// covers the standard CalDAV shape for "delete this already-modified
// Occurrence": the Override VEVENT drops out of the object and an EXDATE
// line appears for its RECURRENCE-ID, both in the same PUT. This is not the
// "revert to the rule" refusal above — it's an ordinary cancellation, and
// must reach Google as one.
func TestEventService_PutSeries_OverrideRemovedWithMatchingExdate_CancelsInstance(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	exposeLinkedCalendar(t, g, userID, calendarID)
	master := seedLinkedRecurringMaster(t, g, userID, calendarID, "put-override-delete-master")
	recurrenceID := master.Start.AddDate(0, 0, 7)
	seedLinkedOverride(t, g, userID, calendarID, master.ID, "put-override-delete-override", recurrenceID)

	write := basicSeriesWrite(master.Title, master.Start, master.End)
	write.Rrule = master.Rrule
	write.Exdates = []time.Time{recurrenceID}

	_, overrides, err := g.Events.PutSeries(ctx, userID, calendarID, master.ID, write)
	if err != nil {
		t.Fatalf("put series: %v", err)
	}
	if len(overrides) != 0 {
		t.Fatalf("expected the override row to be gone, got %+v", overrides)
	}

	msg := soleInstanceWriteBack(t, g, master.ID, repository.OutboxMethodCancelInstance)
	if !msg.WriteBackInstance.RecurrenceID.Equal(recurrenceID) {
		t.Fatalf("expected recurrence id %v, got %v", recurrenceID, msg.WriteBackInstance.RecurrenceID)
	}
}

func TestEventService_PutSeries_DeadConnection_ReturnsNeedsReconnect(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	exposeLinkedCalendar(t, g, userID, calendarID)
	master := seedLinkedMaster(t, g, userID, calendarID, "put-dead-conn-master")

	cal, err := g.Calendars.GetByIDUnchecked(ctx, calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if err := g.ConnectionRepo.UpdateStatus(ctx, userID, *cal.Source.ConnectionID, repository.ConnectionStatusRevoked); err != nil {
		t.Fatalf("revoke connection: %v", err)
	}

	_, _, err = g.Events.PutSeries(ctx, userID, calendarID, master.ID, basicSeriesWrite("Renamed", master.Start, master.End))
	if !errors.Is(err, ErrConnectionNeedsReconnect) {
		t.Fatalf("expected ErrConnectionNeedsReconnect, got %v", err)
	}
	if pending := pendingWriteBacks(t, g, master.ID); len(pending) != 0 {
		t.Fatalf("expected no write-back queued, got %+v", pending)
	}
}

// TestEventService_PutSeries_PreservesProviderOwnedFields covers this
// ticket's own acceptance criterion, depending on #298: a phone's edit
// (a CalDAV PUT) must not blank the Provider RSVP, Conference URL, or guest
// count a Refresh previously stored, the same way EventService.Update
// already can't (#298 removed the columns from EventRepository.Update's SQL
// entirely; PutSeries' own upsertSeries call passes providerOwned=false,
// exactly as an ordinary edit does).
func TestEventService_PutSeries_PreservesProviderOwnedFields(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	exposeLinkedCalendar(t, g, userID, calendarID)
	master := seedLinkedMaster(t, g, userID, calendarID, "put-preserve-provider-fields")

	_, _, err := g.Events.PutSeries(ctx, userID, calendarID, master.ID, basicSeriesWrite("Standup (renamed from a phone)", master.Start, master.End))
	if err != nil {
		t.Fatalf("put series: %v", err)
	}

	got, err := g.EventRepo.GetByID(ctx, master.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.RSVPStatus == nil || *got.RSVPStatus != "accepted" {
		t.Fatalf("expected RSVP status to survive, got %v", got.RSVPStatus)
	}
	if got.ConferenceURL == nil || *got.ConferenceURL != "https://meet.example.com/abc" {
		t.Fatalf("expected conference url to survive, got %v", got.ConferenceURL)
	}
	if got.GuestCount != 3 {
		t.Fatalf("expected guest count to survive, got %d", got.GuestCount)
	}
}
