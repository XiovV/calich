package service

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// This file covers the three recurring-edit scopes reaching a writable Linked
// Calendar's Provider (#293, ADR-0078) at the EventService seam: which outbox
// row each scope queues, and that a read-only Source or a dead Connection
// still refuses every one of them. The send side — resolving the instance id
// from events.instances and the PATCH actually reaching Google — is
// connection_scoped_writeback_test.go.

// seedLinkedRecurringMaster is seedLinkedMaster with a weekly rule, so the
// scoped-edit paths that require a recurring parent have one.
func seedLinkedRecurringMaster(t *testing.T, g *Graph, userID int64, calendarID, id string) repository.Event {
	t.Helper()
	master := seedLinkedMaster(t, g, userID, calendarID, id)
	updated, err := g.EventRepo.Update(context.Background(), master.ID, repository.EventFields{
		CalendarID:   calendarID,
		Title:        master.Title,
		Start:        master.Start,
		End:          master.End,
		Rrule:        "FREQ=WEEKLY",
		ExternalUID:  master.ExternalUID,
		ProviderEtag: master.ProviderEtag,
	}, master.ChangeSeq, master.Sequence)
	if err != nil {
		t.Fatalf("give the seeded master a rule: %v", err)
	}
	return updated
}

// seedLinkedOverride inserts an Override of master at recurrenceID, as a
// Refresh would have — carrying the series' shared ExternalUID and its own
// per-instance etag.
func seedLinkedOverride(t *testing.T, g *Graph, userID int64, calendarID, masterID, id string, recurrenceID time.Time) repository.Event {
	t.Helper()
	ctx := context.Background()
	seq, err := g.SyncRepo.NextChangeSeq(ctx)
	if err != nil {
		t.Fatalf("next change seq: %v", err)
	}
	externalUID := "google-event-1"
	etag := "instance-etag-seed"
	ev, err := g.EventRepo.Create(ctx, id, &userID, repository.EventFields{
		CalendarID:   calendarID,
		Title:        "Standup (moved)",
		Start:        recurrenceID.Add(time.Hour),
		End:          recurrenceID.Add(90 * time.Minute),
		ParentID:     &masterID,
		RecurrenceID: &recurrenceID,
		ExternalUID:  &externalUID,
		ProviderEtag: &etag,
	}, seq)
	if err != nil {
		t.Fatalf("seed linked override: %v", err)
	}
	return ev
}

func soleInstanceWriteBack(t *testing.T, g *Graph, masterID, wantMethod string) repository.OutboxMessage {
	t.Helper()
	pending := pendingWriteBacks(t, g, masterID)
	if len(pending) != 1 {
		t.Fatalf("expected exactly one pending write-back for master %q, got %d", masterID, len(pending))
	}
	if pending[0].Method != wantMethod {
		t.Fatalf("expected method %q, got %q", wantMethod, pending[0].Method)
	}
	if pending[0].WriteBackInstance == nil {
		t.Fatalf("expected an instance snapshot on the write-back row, got %+v", pending[0])
	}
	return pending[0]
}

func TestEventService_ScopedEdit_ThisEvent_NewOverride_EnqueuesInstancePatch(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	master := seedLinkedRecurringMaster(t, g, userID, calendarID, "evt-master-this")

	recurrenceID := master.Start.AddDate(0, 0, 7)
	override, err := g.Events.Create(ctx, userID, "evt-override-this", EventWrite{
		CalendarID:   calendarID,
		Title:        "Standup (this week only)",
		Start:        recurrenceID.Add(time.Hour),
		End:          recurrenceID.Add(90 * time.Minute),
		ParentID:     &master.ID,
		RecurrenceID: &recurrenceID,
	})
	if err != nil {
		t.Fatalf("create override on a writable linked calendar: %v", err)
	}

	msg := soleInstanceWriteBack(t, g, master.ID, repository.OutboxMethodInstance)
	if msg.WriteBackInstance.MasterEventID != master.ID {
		t.Fatalf("expected the snapshot to name the master, got %q", msg.WriteBackInstance.MasterEventID)
	}
	if !msg.WriteBackInstance.RecurrenceID.Equal(recurrenceID) {
		t.Fatalf("expected the snapshot's recurrence id %v, got %v", recurrenceID, msg.WriteBackInstance.RecurrenceID)
	}
	if msg.WriteBackInstance.OverrideEventID != override.ID {
		t.Fatalf("expected the snapshot to name the new override, got %q", msg.WriteBackInstance.OverrideEventID)
	}
	if msg.EventID != master.ID {
		t.Fatalf("expected event_id to be the master's id (so the reconciler protects the whole series), got %q", msg.EventID)
	}
}

func TestEventService_ScopedEdit_ThisEvent_ExistingOverride_EnqueuesInstancePatch(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	master := seedLinkedRecurringMaster(t, g, userID, calendarID, "evt-master-existing")
	recurrenceID := master.Start.AddDate(0, 0, 7)
	override := seedLinkedOverride(t, g, userID, calendarID, master.ID, "evt-override-existing", recurrenceID)

	if _, err := g.Events.Update(ctx, userID, override.ID, EventWrite{
		CalendarID: calendarID,
		Title:      "Standup (retitled this week)",
		Start:      override.Start,
		End:        override.End,
	}); err != nil {
		t.Fatalf("update an existing override on a writable linked calendar: %v", err)
	}

	msg := soleInstanceWriteBack(t, g, master.ID, repository.OutboxMethodInstance)
	if msg.WriteBackInstance.OverrideEventID != override.ID {
		t.Fatalf("expected the snapshot to name the edited override, got %q", msg.WriteBackInstance.OverrideEventID)
	}
}

func TestEventService_ScopedEdit_DeleteThisEvent_AddException_EnqueuesCancelInstance(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	master := seedLinkedRecurringMaster(t, g, userID, calendarID, "evt-master-exdate")
	recurrenceID := master.Start.AddDate(0, 0, 14)

	if err := g.Events.AddException(ctx, userID, master.ID, recurrenceID); err != nil {
		t.Fatalf("add exception on a writable linked calendar: %v", err)
	}

	msg := soleInstanceWriteBack(t, g, master.ID, repository.OutboxMethodCancelInstance)
	if msg.WriteBackInstance.OverrideEventID != "" {
		t.Fatalf("expected no override id on an AddException cancel, got %q", msg.WriteBackInstance.OverrideEventID)
	}
	if !msg.WriteBackInstance.RecurrenceID.Equal(recurrenceID) {
		t.Fatalf("expected the cancelled occurrence's start %v, got %v", recurrenceID, msg.WriteBackInstance.RecurrenceID)
	}
}

func TestEventService_ScopedEdit_DeleteThisEvent_OverrideDelete_EnqueuesCancelInstance(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	master := seedLinkedRecurringMaster(t, g, userID, calendarID, "evt-master-odel")
	recurrenceID := master.Start.AddDate(0, 0, 21)
	override := seedLinkedOverride(t, g, userID, calendarID, master.ID, "evt-override-odel", recurrenceID)

	if err := g.Events.Delete(ctx, userID, override.ID); err != nil {
		t.Fatalf("delete an override on a writable linked calendar: %v", err)
	}

	msg := soleInstanceWriteBack(t, g, master.ID, repository.OutboxMethodCancelInstance)
	if msg.WriteBackInstance.OverrideEventID != override.ID {
		t.Fatalf("expected the snapshot to name the deleted override, got %q", msg.WriteBackInstance.OverrideEventID)
	}
}

func TestEventService_ScopedEdit_ThisAndFollowing_ReparentFrom_Allowed_QueuesNothing(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	oldMaster := seedLinkedRecurringMaster(t, g, userID, calendarID, "evt-old-master")

	// The frontend split has already run update(old) + create(new); this is
	// just the reparent, which is a purely local row move on a Linked Calendar
	// and must not itself queue a Provider push (#293, ADR-0078).
	seq, err := g.SyncRepo.NextChangeSeq(ctx)
	if err != nil {
		t.Fatalf("next change seq: %v", err)
	}
	newMaster, err := g.EventRepo.Create(ctx, "evt-new-master", &userID, repository.EventFields{
		CalendarID: calendarID, Title: "Standup", Start: oldMaster.Start.AddDate(0, 1, 0), End: oldMaster.End.AddDate(0, 1, 0), Rrule: "FREQ=WEEKLY",
	}, seq)
	if err != nil {
		t.Fatalf("seed new master: %v", err)
	}

	if err := g.Events.ReparentFrom(ctx, userID, oldMaster.ID, newMaster.ID, oldMaster.Start.AddDate(0, 1, 0)); err != nil {
		t.Fatalf("reparent on a writable linked calendar: %v", err)
	}

	if p := pendingWriteBacks(t, g, oldMaster.ID); len(p) != 0 {
		t.Fatalf("expected no write-back for the old master, got %+v", p)
	}
	if p := pendingWriteBacks(t, g, newMaster.ID); len(p) != 0 {
		t.Fatalf("expected no write-back for the new master, got %+v", p)
	}
}

// TestEventService_ScopedEdit_RefusedOnReadOnlyLinkedCalendar and
// _RefusedOnDeadConnection: every scope is refused the same way an ordinary
// Master edit already is (event_writeback_test.go) — the Access clamp for a
// read-only Source, ErrConnectionNeedsReconnect for a Connection off Live —
// and never partly commits.
func TestEventService_ScopedEdit_RefusedOnReadOnlyLinkedCalendar(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeReadOnly)
	master := seedLinkedRecurringMaster(t, g, userID, calendarID, "evt-ro-master")
	recurrenceID := master.Start.AddDate(0, 0, 7)

	_, createErr := g.Events.Create(ctx, userID, "evt-ro-override", EventWrite{
		CalendarID: calendarID, Title: "x", Start: recurrenceID, End: recurrenceID.Add(time.Hour),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	})
	if createErr != ErrCalendarReadOnly {
		t.Fatalf("Create override: expected ErrCalendarReadOnly, got %v", createErr)
	}
	if err := g.Events.AddException(ctx, userID, master.ID, recurrenceID); err != ErrCalendarReadOnly {
		t.Fatalf("AddException: expected ErrCalendarReadOnly, got %v", err)
	}
	if err := g.Events.ReparentFrom(ctx, userID, master.ID, master.ID, recurrenceID); err != ErrCalendarReadOnly {
		t.Fatalf("ReparentFrom: expected ErrCalendarReadOnly, got %v", err)
	}
}

// TestEventService_ScopedEdit_RefusesOverrideOfNonRecurringLinkedMaster: an
// Override needs a Provider instance to PATCH, and events.instances only
// answers for a recurring event — so a non-recurring parent is refused
// synchronously (as AddException already does) rather than queued to fail and
// raise the Source into needs-attention.
func TestEventService_ScopedEdit_RefusesOverrideOfNonRecurringLinkedMaster(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	master := seedLinkedMaster(t, g, userID, calendarID, "evt-nonrecurring-master") // no rule
	recurrenceID := master.Start.AddDate(0, 0, 7)

	_, err := g.Events.Create(ctx, userID, "evt-doomed-override", EventWrite{
		CalendarID: calendarID, Title: "x", Start: recurrenceID, End: recurrenceID.Add(time.Hour),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	})
	if err != ErrParentNotRecurring {
		t.Fatalf("expected ErrParentNotRecurring, got %v", err)
	}
	if p := pendingWriteBacks(t, g, master.ID); len(p) != 0 {
		t.Fatalf("expected nothing queued, got %+v", p)
	}
}

func TestEventService_ScopedEdit_RefusedOnDeadConnection(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	master := seedLinkedRecurringMaster(t, g, userID, calendarID, "evt-dead-master")
	recurrenceID := master.Start.AddDate(0, 0, 7)
	override := seedLinkedOverride(t, g, userID, calendarID, master.ID, "evt-dead-override", recurrenceID)

	source, err := g.SourceRepo.GetByCalendarID(ctx, calendarID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if err := g.ConnectionRepo.UpdateStatus(ctx, userID, *source.ConnectionID, repository.ConnectionStatusExpired); err != nil {
		t.Fatalf("expire connection: %v", err)
	}

	if _, err := g.Events.Create(ctx, userID, "evt-dead-new-override", EventWrite{
		CalendarID: calendarID, Title: "x", Start: recurrenceID, End: recurrenceID.Add(time.Hour),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	}); err != ErrConnectionNeedsReconnect {
		t.Fatalf("Create override: expected ErrConnectionNeedsReconnect, got %v", err)
	}
	if _, err := g.Events.Update(ctx, userID, override.ID, EventWrite{
		CalendarID: calendarID, Title: "y", Start: override.Start, End: override.End,
	}); err != ErrConnectionNeedsReconnect {
		t.Fatalf("Update override: expected ErrConnectionNeedsReconnect, got %v", err)
	}
	if err := g.Events.AddException(ctx, userID, master.ID, master.Start.AddDate(0, 0, 14)); err != ErrConnectionNeedsReconnect {
		t.Fatalf("AddException: expected ErrConnectionNeedsReconnect, got %v", err)
	}
	if err := g.Events.Delete(ctx, userID, override.ID); err != ErrConnectionNeedsReconnect {
		t.Fatalf("Delete override: expected ErrConnectionNeedsReconnect, got %v", err)
	}

	if p := pendingWriteBacks(t, g, master.ID); len(p) != 0 {
		t.Fatalf("expected nothing queued against a dead connection, got %+v", p)
	}
}
