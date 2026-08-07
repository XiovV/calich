package service

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calendar/server/internal/repository"
)

// overrideFor asserts through EventService's own public read path —
// ListAllWithReminders, the firing engine's fan-out data — rather than
// reaching into EventService's unexported reminderOverrides field, mirroring
// how the rest of this package's tests assert through public API.
func overrideFor(t *testing.T, events *EventService, userID int64, eventID string) (repository.ReminderOverride, bool) {
	t.Helper()
	all, err := events.ListAllWithReminders(context.Background())
	if err != nil {
		t.Fatalf("list all with reminders: %v", err)
	}
	for _, e := range all {
		if e.ID == eventID {
			o, ok := e.Overrides[userID]
			return o, ok
		}
	}
	return repository.ReminderOverride{}, false
}

// TestEventService_Viewer_CanSetReminderOverride covers #105's acceptance
// criterion "A User with only Viewer Access can still set an override for
// themselves" — the guard is getOwnedEvent's CanRead check, not
// requireWritableCalendar's CanWrite one.
func TestEventService_Viewer_CanSetReminderOverride(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{
		CalendarID: f.calendarID, Title: "Bin day", Start: shareTestStart, End: shareTestEnd,
		Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	offset := 120
	override, err := f.events.SetReminderOverride(ctx, f.viewerID, event.ID, ReminderOverrideWrite{OffsetMinutes: &offset})
	if err != nil {
		t.Fatalf("viewer set override: %v", err)
	}
	if override.OffsetMinutes == nil || *override.OffsetMinutes != 120 {
		t.Fatalf("unexpected override: %+v", override)
	}
}

// GetReminderOverride reports found=false, not an error, when the caller
// has never set one — the fallback case is a normal outcome, not a failure.
func TestEventService_GetReminderOverride_NotFoundWhenUnset(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, found, err := f.events.GetReminderOverride(ctx, f.viewerID, event.ID)
	if err != nil {
		t.Fatalf("get override: %v", err)
	}
	if found {
		t.Fatalf("expected found=false when the caller never set an override")
	}
}

// GetReminderOverride returns exactly what SetReminderOverride wrote —
// a client can read the current override back before changing just one
// field of it, since SetReminderOverride replaces it wholesale.
func TestEventService_GetReminderOverride_ReturnsWhatWasSet(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	offset := 45
	if _, err := f.events.SetReminderOverride(ctx, f.viewerID, event.ID, ReminderOverrideWrite{OffsetMinutes: &offset}); err != nil {
		t.Fatalf("set override: %v", err)
	}

	got, found, err := f.events.GetReminderOverride(ctx, f.viewerID, event.ID)
	if err != nil {
		t.Fatalf("get override: %v", err)
	}
	if !found || got.OffsetMinutes == nil || *got.OffsetMinutes != 45 {
		t.Fatalf("unexpected override: %+v (found=%v)", got, found)
	}
}

// A stranger with no Access at all gets not-found, matching every other
// Event operation's convention (ADR-0034).
func TestEventService_Stranger_CannotSetReminderOverride(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := f.events.SetReminderOverride(ctx, f.strangerID, event.ID, ReminderOverrideWrite{Muted: true}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("stranger set override err = %v, want ErrNotFound", err)
	}
}

// Muting is set via the same call, and cleared via ClearReminderOverride,
// falling back to the Event's own Reminders.
func TestEventService_SetAndClearReminderOverride(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{
		CalendarID: f.calendarID, Title: "Bin day", Start: shareTestStart, End: shareTestEnd,
		Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := f.events.SetReminderOverride(ctx, f.editorID, event.ID, ReminderOverrideWrite{Muted: true}); err != nil {
		t.Fatalf("mute: %v", err)
	}
	got, ok := overrideFor(t, f.events, f.editorID, event.ID)
	if !ok || !got.Muted {
		t.Fatalf("expected muted override, got %+v (present=%v)", got, ok)
	}

	if err := f.events.ClearReminderOverride(ctx, f.editorID, event.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := overrideFor(t, f.events, f.editorID, event.ID); ok {
		t.Fatalf("expected override gone after clear")
	}
}

// An override never bumps the Event's change sequence or touches its own
// Reminders — it is a delivery preference layered on top, not a write to
// the Event (ADR-0036).
func TestEventService_SetReminderOverride_DoesNotChangeEventOrChangeSeq(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{
		CalendarID: f.calendarID, Title: "Bin day", Start: shareTestStart, End: shareTestEnd,
		Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before, err := f.events.Get(ctx, f.ownerID, event.ID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}

	if _, err := f.events.SetReminderOverride(ctx, f.editorID, event.ID, ReminderOverrideWrite{Muted: true}); err != nil {
		t.Fatalf("set override: %v", err)
	}

	after, err := f.events.Get(ctx, f.ownerID, event.ID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if after.ChangeSeq != before.ChangeSeq {
		t.Fatalf("change_seq changed from %d to %d after setting a reminder override", before.ChangeSeq, after.ChangeSeq)
	}
	if len(after.Reminders) != 1 || after.Reminders[0].OffsetMinutes != 10 {
		t.Fatalf("expected the Event's own Reminders unchanged, got %+v", after.Reminders)
	}
}

// An invalid Channel is rejected the same way Event.Reminders' own Channel
// validation rejects one.
func TestEventService_SetReminderOverride_RejectsInvalidChannel(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	invalid := "sms"
	if _, err := f.events.SetReminderOverride(ctx, f.ownerID, event.ID, ReminderOverrideWrite{Channel: &invalid}); !errors.Is(err, ErrInvalidReminderChannel) {
		t.Fatalf("set override with invalid channel err = %v, want ErrInvalidReminderChannel", err)
	}
}

// Revoking a Share clears the revoked User's Reminder overrides on that
// Calendar's Events — #105's "an override is removed when the User loses
// Access to the Calendar".
func TestEventService_RevokeShare_ClearsReminderOverride(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{
		CalendarID: f.calendarID, Title: "Bin day", Start: shareTestStart, End: shareTestEnd,
		Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.events.SetReminderOverride(ctx, f.viewerID, event.ID, ReminderOverrideWrite{Muted: true}); err != nil {
		t.Fatalf("set override: %v", err)
	}

	if err := f.calendars.RevokeShare(ctx, f.ownerID, f.calendarID, f.viewerID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if _, ok := overrideFor(t, f.events, f.viewerID, event.ID); ok {
		t.Fatalf("expected override cleared after revoke")
	}
}

// LeaveShare mirrors RevokeShare's cleanup for a User renouncing their own
// Access.
func TestEventService_LeaveShare_ClearsReminderOverride(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{
		CalendarID: f.calendarID, Title: "Bin day", Start: shareTestStart, End: shareTestEnd,
		Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.events.SetReminderOverride(ctx, f.editorID, event.ID, ReminderOverrideWrite{Muted: true}); err != nil {
		t.Fatalf("set override: %v", err)
	}

	if err := f.calendars.LeaveShare(ctx, f.editorID, f.calendarID); err != nil {
		t.Fatalf("leave: %v", err)
	}

	if _, ok := overrideFor(t, f.events, f.editorID, event.ID); ok {
		t.Fatalf("expected override cleared after leave")
	}
}

// An override survives an Event update to its other fields — it is keyed on
// event_id, not on any particular Reminder row, so ReplaceByEventID's
// wholesale swap of event_reminders on Update leaves it untouched
// (ADR-0036, ADR-0020).
func TestEventService_ReminderOverride_SurvivesEventUpdate(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{
		CalendarID: f.calendarID, Title: "Bin day", Start: shareTestStart, End: shareTestEnd,
		Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.events.SetReminderOverride(ctx, f.viewerID, event.ID, ReminderOverrideWrite{Muted: true}); err != nil {
		t.Fatalf("set override: %v", err)
	}

	if _, err := f.events.Update(ctx, f.ownerID, event.ID, EventWrite{
		CalendarID: f.calendarID, Title: "Bin day (renamed)", Start: shareTestStart, End: shareTestEnd,
		Reminders: []repository.Reminder{{OffsetMinutes: 30, Channel: "email"}},
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, ok := overrideFor(t, f.events, f.viewerID, event.ID)
	if !ok || !got.Muted {
		t.Fatalf("expected the override to survive the Event update, got %+v (present=%v)", got, ok)
	}
}

// The firing engine's read path (ListAllWithReminders) surfaces the
// override so Due can apply it — an end-to-end check that SetReminderOverride
// actually reaches the fan-out data ListAllWithReminders builds.
func TestEventService_ListAllWithReminders_IncludesOverride(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{
		CalendarID: f.calendarID, Title: "Bin day", Start: shareTestStart, End: shareTestEnd,
		Reminders: []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.events.SetReminderOverride(ctx, f.viewerID, event.ID, ReminderOverrideWrite{Muted: true}); err != nil {
		t.Fatalf("set override: %v", err)
	}

	events, err := f.events.ListAllWithReminders(ctx)
	if err != nil {
		t.Fatalf("list all with reminders: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	override, ok := events[0].Overrides[f.viewerID]
	if !ok || !override.Muted {
		t.Fatalf("expected viewer's muted override to be attached, got %+v", events[0].Overrides)
	}
}
