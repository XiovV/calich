package service

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// newTestLinkedCalendar creates a Calendar carrying a Connection-kind Source
// in mode, owned by a fresh user in a fresh workspace — the fixture every
// write-back test in this file needs, without going through
// ConnectionService's own OAuth/Google plumbing (event.go's Update doesn't
// care how the Source got there, only what it says).
func newTestLinkedCalendar(t *testing.T, g *Graph, mode repository.SourceMode) (userID int64, calendarID string) {
	t.Helper()
	ctx := context.Background()

	user, err := g.UserRepo.Create(ctx, "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspace, err := g.WorkspaceRepo.Create(ctx, "Test Workspace", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := g.WorkspaceRepo.AddMember(ctx, workspace.ID, user.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	cal, err := g.CalendarRepo.Create(ctx, user.ID, workspace.ID, "cal-linked", repository.CalendarFields{Name: "Work", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	conn, err := g.ConnectionRepo.Upsert(ctx, user.ID, repository.ProviderGoogle, "someone@gmail.com", repository.ConnectionFields{
		RefreshToken: "encrypted-refresh", Status: repository.ConnectionStatusLive,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}
	externalCalendarID := "primary"
	if _, err := g.SourceRepo.Create(ctx, cal.ID, repository.SourceFields{
		Kind:               repository.SourceKindConnection,
		Mode:               mode,
		ConnectionID:       &conn.ID,
		ExternalCalendarID: &externalCalendarID,
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}

	return user.ID, cal.ID
}

// seedLinkedMaster inserts a Master row exactly as a Refresh would have left
// one: an ExternalUID, a ProviderEtag, and the Provider-owned fields
// EventWrite has no way to set — going around EventService.Create, which
// (correctly) exposes none of these to an ordinary caller.
func seedLinkedMaster(t *testing.T, g *Graph, userID int64, calendarID, id string) repository.Event {
	t.Helper()
	ctx := context.Background()

	seq, err := g.SyncRepo.NextChangeSeq(ctx)
	if err != nil {
		t.Fatalf("next change seq: %v", err)
	}
	externalUID := "google-event-1"
	etag := "etag-1"
	rsvp := "accepted"
	conferenceURL := "https://meet.example.com/abc"
	event, err := g.EventRepo.Create(ctx, id, &userID, repository.EventFields{
		CalendarID:    calendarID,
		Title:         "Standup",
		Start:         time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		End:           time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC),
		ExternalUID:   &externalUID,
		ProviderEtag:  &etag,
		RSVPStatus:    &rsvp,
		ConferenceURL: &conferenceURL,
		GuestCount:    3,
	}, seq)
	if err != nil {
		t.Fatalf("seed linked master: %v", err)
	}
	return event
}

// pendingWriteBacks returns every pending OutboxKindWriteBack row for
// eventID, in whatever order ListPending returns them.
func pendingWriteBacks(t *testing.T, g *Graph, eventID string) []repository.OutboxMessage {
	t.Helper()
	all, err := g.OutboxRepo.ListPending(context.Background(), 200)
	if err != nil {
		t.Fatalf("list pending outbox messages: %v", err)
	}
	var matched []repository.OutboxMessage
	for _, m := range all {
		if m.EventID == eventID && m.Kind == repository.OutboxKindWriteBack {
			matched = append(matched, m)
		}
	}
	return matched
}

// TestEventService_Update_EnqueuesWriteBackOnWritableLinkedCalendar covers
// #290's first two acceptance-criteria bullets: editing a Master on a
// writable Linked Calendar enqueues a PATCH write-back, and it lands in the
// outbox alongside the local write.
func TestEventService_Update_EnqueuesWriteBackOnWritableLinkedCalendar(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	event := seedLinkedMaster(t, g, userID, calendarID, "evt-linked-1")

	updated, err := g.Events.Update(context.Background(), userID, event.ID, EventWrite{
		CalendarID: calendarID,
		Title:      "Standup (renamed)",
		Start:      event.Start,
		End:        event.End,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "Standup (renamed)" {
		t.Fatalf("expected the local write to apply, got %+v", updated)
	}

	pending := pendingWriteBacks(t, g, event.ID)
	if len(pending) != 1 {
		t.Fatalf("expected exactly one pending write-back, got %d", len(pending))
	}
	if pending[0].Method != repository.OutboxMethodPatch {
		t.Fatalf("expected method %q, got %q", repository.OutboxMethodPatch, pending[0].Method)
	}
	if pending[0].RecipientUserID != nil || pending[0].RecipientEmail != nil {
		t.Fatalf("expected a write-back row to carry no recipient, got %+v", pending[0])
	}
}

// TestEventService_Update_NeverEnqueuesWriteBackOnAnOrdinaryCalendar is the
// regression guard: an Event on a Calendar with no Source at all must never
// queue a Provider push, whatever its ExternalUID happens to be.
func TestEventService_Update_NeverEnqueuesWriteBackOnAnOrdinaryCalendar(t *testing.T) {
	g := newTestGraph(t)
	svc, userID, calendarID := newTestEventServiceOnGraph(t, g)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-ordinary-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.Update(ctx, userID, event.ID, EventWrite{CalendarID: calendarID, Title: "Standup (renamed)", Start: start, End: end}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if pending := pendingWriteBacks(t, g, event.ID); len(pending) != 0 {
		t.Fatalf("expected no write-back queued for an ordinary calendar, got %+v", pending)
	}
}

// newTestEventServiceOnGraph is newTestEventService's sibling for a test
// that already built its own Graph (to reach g.OutboxRepo afterward) rather
// than letting newTestEventService build one internally.
func newTestEventServiceOnGraph(t *testing.T, g *Graph) (svc *EventService, userID int64, calendarID string) {
	t.Helper()
	ctx := context.Background()

	user, err := g.UserRepo.Create(ctx, "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspace, err := g.WorkspaceRepo.Create(ctx, "Test Workspace", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := g.WorkspaceRepo.AddMember(ctx, workspace.ID, user.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	cal, err := g.CalendarRepo.Create(ctx, user.ID, workspace.ID, "cal-1", repository.CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	return g.Events, user.ID, cal.ID
}

// TestEventService_Update_RefusesEditOnAReadOnlyLinkedCalendar covers #290's
// acceptance-criteria bullet: a Linked Calendar the Provider reports
// read-only refuses the edit, via the same Access clamp a Subscription's
// read-only Source already enforces (access.go).
func TestEventService_Update_RefusesEditOnAReadOnlyLinkedCalendar(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeReadOnly)
	event := seedLinkedMaster(t, g, userID, calendarID, "evt-linked-2")

	_, err := g.Events.Update(context.Background(), userID, event.ID, EventWrite{
		CalendarID: calendarID, Title: "Should not apply", Start: event.Start, End: event.End,
	})
	if err != ErrCalendarReadOnly {
		t.Fatalf("expected ErrCalendarReadOnly, got %v", err)
	}
}

// TestEventService_Create_RefusesOnReadOnlyLinkedCalendar and
// TestEventService_Delete_RefusesOnReadOnlyLinkedCalendar: a Linked Calendar
// the Provider reports read-only refuses both, via the same Access clamp
// (access.go) a Subscription's read-only Source enforces. Create/delete of a
// plain Master on a *writable* one is now supported (#292) — see
// event_create_delete_writeback_test.go.
func TestEventService_Create_RefusesOnReadOnlyLinkedCalendar(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeReadOnly)

	_, err := g.Events.Create(context.Background(), userID, "evt-new-on-linked", EventWrite{
		CalendarID: calendarID,
		Title:      "Should not be created",
		Start:      time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		End:        time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC),
	})
	if err != ErrCalendarReadOnly {
		t.Fatalf("expected ErrCalendarReadOnly, got %v", err)
	}
}

func TestEventService_Delete_RefusesOnReadOnlyLinkedCalendar(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeReadOnly)
	event := seedLinkedMaster(t, g, userID, calendarID, "evt-linked-delete")

	if err := g.Events.Delete(context.Background(), userID, event.ID); err != ErrCalendarReadOnly {
		t.Fatalf("expected ErrCalendarReadOnly, got %v", err)
	}
}

// TestEventService_Create_RefusesAttendeesOnWritableLinkedCalendar keeps the
// one part of Create still refused on a writable Linked Calendar (#292,
// ADR-0052): a Linked Calendar's Events carry no Attendees this app mirrors
// to the Provider, so inviting one here would mint a local-only row
// write-back has no way to reconcile against Google's guest list.
func TestEventService_Create_RefusesAttendeesOnWritableLinkedCalendar(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	target, err := g.UserRepo.Create(context.Background(), "target-user", "target@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create target user: %v", err)
	}

	_, err = g.Events.Create(context.Background(), userID, "evt-new-with-attendee", EventWrite{
		CalendarID:      calendarID,
		Title:           "Should not be created",
		Start:           time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		End:             time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC),
		AttendeeUserIDs: []int64{target.ID},
	})
	if err != ErrLinkedCalendarWriteUnsupported {
		t.Fatalf("expected ErrLinkedCalendarWriteUnsupported, got %v", err)
	}
}

// The scoped recurring writes AddException and ReparentFrom used to refuse on
// a Connection Source now go through (#293, ADR-0078) — see
// event_scoped_writeback_test.go for their allow-and-enqueue coverage. What
// stays refused is a ReparentFrom that would cross the Provider boundary.
func TestEventService_ReparentFrom_RefusesCrossingTheProviderBoundary(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	userID, linkedCalendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	oldParent := seedLinkedMaster(t, g, userID, linkedCalendarID, "evt-linked-old-parent")

	workspace, err := g.WorkspaceRepo.Create(ctx, "Other Workspace", userID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := g.WorkspaceRepo.AddMember(ctx, workspace.ID, userID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	ordinaryCalendar, err := g.CalendarRepo.Create(ctx, userID, workspace.ID, "cal-ordinary", repository.CalendarFields{Name: "Ordinary", Color: "peacock"})
	if err != nil {
		t.Fatalf("create ordinary calendar: %v", err)
	}
	seq, err := g.SyncRepo.NextChangeSeq(ctx)
	if err != nil {
		t.Fatalf("next change seq: %v", err)
	}
	newParent, err := g.EventRepo.Create(ctx, "evt-ordinary-new-parent", &userID, repository.EventFields{
		CalendarID: ordinaryCalendar.ID, Title: "New parent", Start: oldParent.Start, End: oldParent.End,
	}, seq)
	if err != nil {
		t.Fatalf("seed new parent: %v", err)
	}

	err = g.Events.ReparentFrom(ctx, userID, oldParent.ID, newParent.ID, oldParent.Start)
	if err != ErrLinkedCalendarWriteUnsupported {
		t.Fatalf("expected ErrLinkedCalendarWriteUnsupported, got %v", err)
	}
}

// TestEventService_Update_RefusesMovingEventAcrossConnectionCalendarBoundary
// covers Update's own cross-calendar-move branch: moving an Event into or
// out of a writable Connection Calendar is refused exactly like Create or
// Delete would be, since it's structurally the same operation.
func TestEventService_Update_RefusesMovingEventAcrossConnectionCalendarBoundary(t *testing.T) {
	g := newTestGraph(t)
	userID, linkedCalendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	event := seedLinkedMaster(t, g, userID, linkedCalendarID, "evt-linked-move")

	workspace, err := g.WorkspaceRepo.Create(context.Background(), "Other Workspace", userID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := g.WorkspaceRepo.AddMember(context.Background(), workspace.ID, userID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	ordinaryCalendar, err := g.CalendarRepo.Create(context.Background(), userID, workspace.ID, "cal-ordinary", repository.CalendarFields{Name: "Ordinary", Color: "peacock"})
	if err != nil {
		t.Fatalf("create ordinary calendar: %v", err)
	}

	_, err = g.Events.Update(context.Background(), userID, event.ID, EventWrite{
		CalendarID: ordinaryCalendar.ID, Title: event.Title, Start: event.Start, End: event.End,
	})
	if err != ErrLinkedCalendarWriteUnsupported {
		t.Fatalf("expected ErrLinkedCalendarWriteUnsupported, got %v", err)
	}
}

// TestEventService_Update_CarriesForwardProviderOwnedFields is the bug
// EventFields' own doc comment flags (#290, ADR-0075): a plain field edit —
// here, a title-only one — must never null out ProviderEtag/RSVPStatus/
// ConferenceURL/GuestCount, which write.fields() alone doesn't carry.
func TestEventService_Update_CarriesForwardProviderOwnedFields(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	event := seedLinkedMaster(t, g, userID, calendarID, "evt-linked-3")

	if _, err := g.Events.Update(context.Background(), userID, event.ID, EventWrite{
		CalendarID: calendarID, Title: "Renamed only", Start: event.Start, End: event.End,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := g.EventRepo.GetByID(context.Background(), event.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.RSVPStatus == nil || *got.RSVPStatus != "accepted" {
		t.Fatalf("expected RSVPStatus to survive a title-only edit, got %v", got.RSVPStatus)
	}
	if got.ConferenceURL == nil || *got.ConferenceURL != "https://meet.example.com/abc" {
		t.Fatalf("expected ConferenceURL to survive a title-only edit, got %v", got.ConferenceURL)
	}
	if got.GuestCount != 3 {
		t.Fatalf("expected GuestCount to survive a title-only edit, got %d", got.GuestCount)
	}
	if got.ProviderEtag == nil || *got.ProviderEtag != "etag-1" {
		t.Fatalf("expected ProviderEtag to survive a title-only edit, got %v", got.ProviderEtag)
	}
}

// TestEventService_UpsertSeries_NeverWritesProviderOwnedFieldsWhenNotProviderOwned
// is #298's structural guarantee, exercised directly at upsertSeries' own
// seam rather than through EventService.Update: a write shaped like a
// CalDAV PUT or an ICS import — carrying zero Provider-owned fields, exactly
// what SeriesWrite.fields() produces for every caller but a Refresh
// reconcile — must never blank a Linked Calendar Master's stored Provider
// state. repository.EventRepository.Update has no column for any of the
// five, and upsertSeries only calls the narrow writers that do
// (ApplyProviderOwnedFields, UpdateProviderEtag) when providerOwned is
// true, so this survives by construction rather than by upsertSeries
// remembering to carry the old values forward.
func TestEventService_UpsertSeries_NeverWritesProviderOwnedFieldsWhenNotProviderOwned(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	event := seedLinkedMaster(t, g, userID, calendarID, "evt-linked-upsert")
	ctx := context.Background()

	err := g.Events.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}
		write := SeriesWrite{Title: "Renamed only", Start: event.Start, End: event.End}
		return g.Events.upsertSeries(ctx, repos, calendarID, event.ID, userID, userID, seq, write, false, false)
	})
	if err != nil {
		t.Fatalf("upsert series: %v", err)
	}

	got, err := g.EventRepo.GetByID(ctx, event.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.Title != "Renamed only" {
		t.Fatalf("expected the title to update, got %q", got.Title)
	}
	if got.RSVPStatus == nil || *got.RSVPStatus != "accepted" {
		t.Fatalf("expected RSVPStatus to survive, got %v", got.RSVPStatus)
	}
	if got.ConferenceURL == nil || *got.ConferenceURL != "https://meet.example.com/abc" {
		t.Fatalf("expected ConferenceURL to survive, got %v", got.ConferenceURL)
	}
	if got.GuestCount != 3 {
		t.Fatalf("expected GuestCount to survive, got %d", got.GuestCount)
	}
	if got.ProviderEtag == nil || *got.ProviderEtag != "etag-1" {
		t.Fatalf("expected ProviderEtag to survive, got %v", got.ProviderEtag)
	}
}

// TestEventService_ReconcileSubscribedSeries_MovesProviderOwnedFieldsForwardOnUpdate
// is Full/Delta Refresh's own forward-movement path (#298, ADR-0075):
// ReconcileSubscribedSeries' update branch (providerOwned=true in
// upsertSeries) must still move a fresh Provider fetch's RSVPStatus/
// ConferenceURL/GuestCount/ProviderEtag/ProviderColor onto an existing
// Master, now that repository.EventRepository.Update itself no longer
// touches those columns — this is the acceptance criterion that the
// existing Write-back and Refresh suites must stay green against.
func TestEventService_ReconcileSubscribedSeries_MovesProviderOwnedFieldsForwardOnUpdate(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	event := seedLinkedMaster(t, g, userID, calendarID, "evt-linked-refresh")
	ctx := context.Background()

	newRSVP := "declined"
	newConference := "https://meet.example.com/fresh"
	newEtag := "etag-2"
	newColor := "#abcdef"
	result := ReconcileResult{
		Upserts: []SeriesUpsert{
			{
				MasterID: event.ID,
				Write: SeriesWrite{
					Title: event.Title, Start: event.Start, End: event.End,
					ExternalUID:   *event.ExternalUID,
					ProviderEtag:  &newEtag,
					RSVPStatus:    &newRSVP,
					ConferenceURL: &newConference,
					GuestCount:    7,
					ProviderColor: &newColor,
				},
			},
		},
	}

	summary, err := g.Events.ReconcileSubscribedSeries(ctx, userID, calendarID, result)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if summary.Updated != 1 {
		t.Fatalf("expected 1 update, got %+v", summary)
	}

	got, err := g.EventRepo.GetByID(ctx, event.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.RSVPStatus == nil || *got.RSVPStatus != newRSVP {
		t.Fatalf("expected RSVPStatus to move forward, got %v", got.RSVPStatus)
	}
	if got.ConferenceURL == nil || *got.ConferenceURL != newConference {
		t.Fatalf("expected ConferenceURL to move forward, got %v", got.ConferenceURL)
	}
	if got.GuestCount != 7 {
		t.Fatalf("expected GuestCount to move forward, got %d", got.GuestCount)
	}
	if got.ProviderEtag == nil || *got.ProviderEtag != newEtag {
		t.Fatalf("expected ProviderEtag to move forward, got %v", got.ProviderEtag)
	}
	if got.ProviderColor == nil || *got.ProviderColor != newColor {
		t.Fatalf("expected ProviderColor to move forward, got %v", got.ProviderColor)
	}
}

// TestEventService_UpsertSeries_NeverWritesProviderOwnedFieldsOnOverrideWhenNotProviderOwned
// is the Override branch's own copy of the Master test above (#298): an
// Override's Provider-owned fields live at a different row id than its
// Master's, reached by upsertSeries' separate existing-Override-found
// branch, so it needs its own construction check rather than trusting the
// Master branch's coverage to stand in for it.
func TestEventService_UpsertSeries_NeverWritesProviderOwnedFieldsOnOverrideWhenNotProviderOwned(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	ctx := context.Background()

	masterStart := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	masterID := "evt-linked-master-for-override"
	externalUID := "google-event-series-1"
	seq, err := g.SyncRepo.NextChangeSeq(ctx)
	if err != nil {
		t.Fatalf("next change seq: %v", err)
	}
	if _, err := g.EventRepo.Create(ctx, masterID, &userID, repository.EventFields{
		CalendarID:  calendarID,
		Title:       "Standup",
		Start:       masterStart,
		End:         masterStart.Add(30 * time.Minute),
		Rrule:       "FREQ=DAILY",
		ExternalUID: &externalUID,
	}, seq); err != nil {
		t.Fatalf("seed master: %v", err)
	}

	recurrenceID := masterStart.AddDate(0, 0, 1)
	overrideID := "evt-linked-override"
	overrideStart := recurrenceID.Add(time.Hour)
	rsvp := "accepted"
	conferenceURL := "https://meet.example.com/override"
	etag := "override-etag-1"
	seq, err = g.SyncRepo.NextChangeSeq(ctx)
	if err != nil {
		t.Fatalf("next change seq: %v", err)
	}
	if _, err := g.EventRepo.Create(ctx, overrideID, &userID, repository.EventFields{
		CalendarID:    calendarID,
		Title:         "Standup (moved)",
		Start:         overrideStart,
		End:           overrideStart.Add(30 * time.Minute),
		ParentID:      &masterID,
		RecurrenceID:  &recurrenceID,
		ExternalUID:   &externalUID,
		ProviderEtag:  &etag,
		RSVPStatus:    &rsvp,
		ConferenceURL: &conferenceURL,
		GuestCount:    2,
	}, seq); err != nil {
		t.Fatalf("seed override: %v", err)
	}

	err = g.Events.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}
		write := SeriesWrite{
			Title: "Standup", Start: masterStart, End: masterStart.Add(30 * time.Minute), Rrule: "FREQ=DAILY",
			Overrides: []OverrideWrite{
				{RecurrenceID: recurrenceID, Title: "Standup (renamed only)", Start: overrideStart, End: overrideStart.Add(30 * time.Minute)},
			},
		}
		return g.Events.upsertSeries(ctx, repos, calendarID, masterID, userID, userID, seq, write, false, false)
	})
	if err != nil {
		t.Fatalf("upsert series: %v", err)
	}

	got, err := g.EventRepo.GetByID(ctx, overrideID)
	if err != nil {
		t.Fatalf("get override: %v", err)
	}
	if got.Title != "Standup (renamed only)" {
		t.Fatalf("expected the title to update, got %q", got.Title)
	}
	if got.RSVPStatus == nil || *got.RSVPStatus != "accepted" {
		t.Fatalf("expected override RSVPStatus to survive, got %v", got.RSVPStatus)
	}
	if got.ConferenceURL == nil || *got.ConferenceURL != "https://meet.example.com/override" {
		t.Fatalf("expected override ConferenceURL to survive, got %v", got.ConferenceURL)
	}
	if got.GuestCount != 2 {
		t.Fatalf("expected override GuestCount to survive, got %d", got.GuestCount)
	}
	if got.ProviderEtag == nil || *got.ProviderEtag != "override-etag-1" {
		t.Fatalf("expected override ProviderEtag to survive, got %v", got.ProviderEtag)
	}
}

// TestEventService_PutSeries_RefusesConnectionCalendarMatchingCalDAVsPosture
// covers the CalDAV write seam directly (#290): a CalDAV PUT onto a Linked
// Calendar is refused independent of Exposure (ADR-0080) or the Source's
// mode, since Write-back (ADR-0075) is the only path meant to carry an edit
// to the Provider — but PutCalendarObject itself resolves calendarID
// straight off the request path with no re-check of its own, so a client
// holding a cached or guessed path could otherwise still reach PutSeries
// directly once the Calendar's Source is writable. Expects
// repository.ErrNotFound, matching GetCalendar's own posture (a Calendar
// that "doesn't exist" to CalDAV, not one that exists but refuses the
// write).
func TestEventService_PutSeries_RefusesConnectionCalendarMatchingCalDAVsPosture(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)

	_, _, err := g.Events.PutSeries(context.Background(), userID, calendarID, "evt-put-1", SeriesWrite{
		Title: "Should not be created", Start: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC),
	})
	if err != repository.ErrNotFound {
		t.Fatalf("expected repository.ErrNotFound, got %v", err)
	}
}

// TestEventService_ImportSeries_RefusesConnectionCalendarRegardlessOfMode
// covers ICS import's own writer directly (#290): ImportService.Import's
// own proposeTarget already refuses any Calendar carrying a Source before
// ever reaching this method, but ImportSeries is guarded here too, matching
// every other mutating EventService method's own defense (ADR-0076: an
// import always mints a row with no ExternalUID, which the next Full
// Refresh tombstones).
func TestEventService_ImportSeries_RefusesConnectionCalendarRegardlessOfMode(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)

	_, err := g.Events.ImportSeries(context.Background(), userID, calendarID, []SeriesWrite{{
		Title: "Should not be imported", Start: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC),
	}})
	if err != ErrLinkedCalendarWriteUnsupported {
		t.Fatalf("expected ErrLinkedCalendarWriteUnsupported, got %v", err)
	}
}

// TestEventService_AddAttendee_RefusesConnectionCalendarRegardlessOfMode
// covers Attendee management's shared gate (#290): a Linked Calendar's Event
// carries no Attendees this app mirrors to the Provider (ADR-0052, ADR-0075),
// so inviting one here — even once the Source is writable — would create a
// local-only row write-back has no way to reconcile against Google's guest
// list.
func TestEventService_AddAttendee_RefusesConnectionCalendarRegardlessOfMode(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	event := seedLinkedMaster(t, g, userID, calendarID, "evt-linked-attendee")

	target, err := g.UserRepo.Create(context.Background(), "target-user", "target@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create target user: %v", err)
	}

	_, err = g.Events.AddAttendee(context.Background(), userID, event.ID, target.ID)
	if err != ErrLinkedCalendarWriteUnsupported {
		t.Fatalf("expected ErrLinkedCalendarWriteUnsupported, got %v", err)
	}
}

// TestEventService_Update_RefusesEditOnAConnectionThatNeedsReconnect covers
// #291's own acceptance criterion: a Connection whose Status has moved off
// Live refuses a new edit on its Linked Calendars outright, rather than
// committing it locally and queuing a push that can never reach a grant
// that's dead. Neither the local write nor a write-back enqueue should
// happen.
func TestEventService_Update_RefusesEditOnAConnectionThatNeedsReconnect(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	event := seedLinkedMaster(t, g, userID, calendarID, "evt-linked-expired")

	source, err := g.SourceRepo.GetByCalendarID(context.Background(), calendarID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if err := g.ConnectionRepo.UpdateStatus(context.Background(), userID, *source.ConnectionID, repository.ConnectionStatusExpired); err != nil {
		t.Fatalf("update connection status: %v", err)
	}

	_, err = g.Events.Update(context.Background(), userID, event.ID, EventWrite{
		CalendarID: calendarID, Title: "Should not apply", Start: event.Start, End: event.End,
	})
	if err != ErrConnectionNeedsReconnect {
		t.Fatalf("expected ErrConnectionNeedsReconnect, got %v", err)
	}

	got, err := g.EventRepo.GetByID(context.Background(), event.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.Title != "Standup" {
		t.Fatalf("expected the local write to be refused, got title %q", got.Title)
	}
	if pending := pendingWriteBacks(t, g, event.ID); len(pending) != 0 {
		t.Fatalf("expected no write-back queued against a dead connection, got %+v", pending)
	}
}

// TestEventService_Update_ClearsStaleWriteBackErrorOnFreshEdit covers #291's
// "a fresh edit deserves a fresh chance" rule: an Event carrying a leftover
// permanent-failure marker from a previous push has it cleared the moment a
// new edit re-enqueues another one, rather than leaving a stale "needs
// attention" mark on the grid while the new push is still in flight.
func TestEventService_Update_ClearsStaleWriteBackErrorOnFreshEdit(t *testing.T) {
	g := newTestGraph(t)
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)
	event := seedLinkedMaster(t, g, userID, calendarID, "evt-linked-stale-error")

	if err := g.EventRepo.MarkWriteBackFailed(context.Background(), event.ID, "a conflicting edit at google could not be resolved after several attempts"); err != nil {
		t.Fatalf("mark write-back failed: %v", err)
	}

	if _, err := g.Events.Update(context.Background(), userID, event.ID, EventWrite{
		CalendarID: calendarID, Title: "Standup (edited again)", Start: event.Start, End: event.End,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := g.EventRepo.GetByID(context.Background(), event.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.WriteBackError != nil {
		t.Fatalf("expected the stale write-back error to be cleared by a fresh edit, got %v", *got.WriteBackError)
	}
}
