package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// eventShareFixture is #100's fixture: an Owner's Calendar shared as Editor
// to one User and Viewer to another, plus a stranger with no Access at
// all — covering the demo in #100's description end to end (minus the REST
// layer, exercised separately in handlers).
type eventShareFixture struct {
	events                                  *EventService
	calendars                               *CalendarService
	users                                   *repository.UserRepository
	ownerID, editorID, viewerID, strangerID int64
	calendarID                              string
}

func newEventShareFixture(t *testing.T) eventShareFixture {
	t.Helper()
	ctx := context.Background()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	editor, err := users.Create(ctx, "editor", "editor@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create editor: %v", err)
	}
	viewer, err := users.Create(ctx, "viewer", "viewer@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	stranger, err := users.Create(ctx, "stranger", "stranger@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create stranger: %v", err)
	}

	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(ctx, "Test Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, owner.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	// A Share can only reach someone already inside the Calendar's own
	// Workspace (#159, ADR-0045) — stranger is deliberately left out, since
	// this fixture also covers "no Access at all".
	if err := workspaceRepo.AddMember(ctx, workspace.ID, editor.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add editor as workspace member: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, viewer.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add viewer as workspace member: %v", err)
	}

	calendars := NewCalendarService(repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	cal, err := calendars.Create(ctx, owner.ID, workspace.ID, "cal-1", CalendarWrite{Name: "Family", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	if _, _, err := calendars.Share(ctx, owner.ID, cal.ID, "editor@example.com", repository.RoleEditor); err != nil {
		t.Fatalf("share editor: %v", err)
	}
	if _, _, err := calendars.Share(ctx, owner.ID, cal.ID, "viewer@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share viewer: %v", err)
	}

	events := NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendars, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, repository.NewGroupRepository(sqlDB), repository.NewNotificationRepository(sqlDB))

	return eventShareFixture{
		events: events, calendars: calendars, users: users,
		ownerID: owner.ID, editorID: editor.ID, viewerID: viewer.ID, strangerID: stranger.ID,
		calendarID: cal.ID,
	}
}

var (
	shareTestStart = time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	shareTestEnd   = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
)

// TestEventService_Editor_CanCreateUpdateDelete covers "an Editor can
// create, edit and delete Events in the shared Calendar" (#100).
func TestEventService_Editor_CanCreateUpdateDelete(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.editorID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd})
	if err != nil {
		t.Fatalf("editor create: %v", err)
	}

	// Access is per-Calendar, not per-author (ADR-0034): an Event has no
	// owner of its own, so the Calendar's Owner sees an Editor's Event too.
	if _, err := f.events.Get(ctx, f.ownerID, event.ID); err != nil {
		t.Fatalf("owner reading editor's event: %v", err)
	}

	updated, err := f.events.Update(ctx, f.editorID, event.ID, EventWrite{CalendarID: f.calendarID, Title: "Standup (renamed)", Start: shareTestStart, End: shareTestEnd})
	if err != nil {
		t.Fatalf("editor update: %v", err)
	}
	if updated.Title != "Standup (renamed)" {
		t.Fatalf("title = %q, want %q", updated.Title, "Standup (renamed)")
	}

	if err := f.events.Delete(ctx, f.editorID, event.ID); err != nil {
		t.Fatalf("editor delete: %v", err)
	}
	if _, err := f.events.Get(ctx, f.ownerID, event.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("get after editor's delete = %v, want ErrNotFound", err)
	}
}

// TestEventService_CreatorAttribution_ShowsWhoeverCreatedIt is #118's basic
// case: an Editor's Event carries their id and username even when a
// different User (the Owner) reads it — attribution follows whoever wrote
// the row, not whoever is asking (ADR-0034).
func TestEventService_CreatorAttribution_ShowsWhoeverCreatedIt(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	created, err := f.events.Create(ctx, f.editorID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd})
	if err != nil {
		t.Fatalf("editor create: %v", err)
	}
	if created.CreatedBy == nil || *created.CreatedBy != f.editorID || created.CreatedByName != "editor" {
		t.Fatalf("expected create to attribute to editor (%d), got %+v", f.editorID, created)
	}

	fetched, err := f.events.Get(ctx, f.ownerID, created.ID)
	if err != nil {
		t.Fatalf("owner get: %v", err)
	}
	if fetched.CreatedBy == nil || *fetched.CreatedBy != f.editorID || fetched.CreatedByName != "editor" {
		t.Fatalf("expected fetched attribution to editor (%d), got %+v", f.editorID, fetched)
	}
}

// TestEventService_CreatorAttribution_OmittedWhenCreatorAccountDeleted is
// #118's "renders without attribution rather than breaking" acceptance
// criterion at the service layer: created_by is cleared to nil by ON DELETE
// SET NULL when the creating account is removed (ADR-0034), so
// CreatedByName resolves to empty afterwards instead of erroring on a
// dangling id.
func TestEventService_CreatorAttribution_OmittedWhenCreatorAccountDeleted(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	created, err := f.events.Create(ctx, f.editorID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd})
	if err != nil {
		t.Fatalf("editor create: %v", err)
	}

	if err := f.users.Delete(ctx, f.editorID); err != nil {
		t.Fatalf("delete editor: %v", err)
	}

	fetched, err := f.events.Get(ctx, f.ownerID, created.ID)
	if err != nil {
		t.Fatalf("expected the event to survive its creator's deletion, got %v", err)
	}
	if fetched.CreatedBy != nil || fetched.CreatedByName != "" {
		t.Fatalf("expected no creator attribution once the account is deleted, got %+v", fetched)
	}
}

// TestEventService_Editor_CanScopeEditRecurringSeries covers "an Editor can
// ... [make] scoped edits to a recurring series" (#100) — "This event"
// (an Override) and "This and following" (AddException/ReparentFrom) both
// route through the same requireWritableCalendar guard as a plain write, so
// an Editor on a shared Calendar can do either.
func TestEventService_Editor_CanScopeEditRecurringSeries(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	master, err := f.events.Create(ctx, f.editorID, "master", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd, Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("editor create master: %v", err)
	}

	// "This event": an Override replacing one Occurrence.
	recurrenceID := shareTestStart.AddDate(0, 0, 1)
	override, err := f.events.Create(ctx, f.editorID, "override", EventWrite{
		CalendarID: f.calendarID, Title: "Standup (moved)",
		Start: recurrenceID.Add(time.Hour), End: recurrenceID.Add(90 * time.Minute),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	})
	if err != nil {
		t.Fatalf("editor create override: %v", err)
	}
	if override.ParentID == nil || *override.ParentID != master.ID {
		t.Fatalf("override.ParentID = %v, want %q", override.ParentID, master.ID)
	}

	// "This event" via delete: an Exception suppressing one Occurrence.
	if err := f.events.AddException(ctx, f.editorID, master.ID, shareTestStart.AddDate(0, 0, 2)); err != nil {
		t.Fatalf("editor add exception: %v", err)
	}

	// "This and following": reparenting a split at a boundary date.
	newMaster, err := f.events.Create(ctx, f.editorID, "new-master", EventWrite{CalendarID: f.calendarID, Title: "Standup (new time)", Start: shareTestStart.AddDate(0, 0, 3), End: shareTestEnd.AddDate(0, 0, 3), Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("editor create split master: %v", err)
	}
	if err := f.events.ReparentFrom(ctx, f.editorID, master.ID, newMaster.ID, shareTestStart.AddDate(0, 0, 3)); err != nil {
		t.Fatalf("editor reparent: %v", err)
	}
}

// TestEventService_Viewer_CanRead covers "A Viewer can read the Calendar's
// Events" (#100).
func TestEventService_Viewer_CanRead(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := f.events.Get(ctx, f.viewerID, event.ID); err != nil {
		t.Fatalf("viewer read: %v", err)
	}

	events, err := f.events.List(ctx, f.viewerID, nil, nil)
	if err != nil {
		t.Fatalf("viewer list: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event in viewer's list, got %d", len(events))
	}
}

// TestEventService_Viewer_RefusedEveryWrite covers "... and is refused
// every write" (#100) — Create, Update, and Delete each return
// ErrCalendarReadOnly for a Viewer.
func TestEventService_Viewer_RefusedEveryWrite(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := f.events.Create(ctx, f.viewerID, "evt-2", EventWrite{CalendarID: f.calendarID, Title: "Nope", Start: shareTestStart, End: shareTestEnd}); !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("viewer create err = %v, want ErrCalendarReadOnly", err)
	}
	if _, err := f.events.Update(ctx, f.viewerID, event.ID, EventWrite{CalendarID: f.calendarID, Title: "Nope", Start: shareTestStart, End: shareTestEnd}); !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("viewer update err = %v, want ErrCalendarReadOnly", err)
	}
	if err := f.events.Delete(ctx, f.viewerID, event.ID); !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("viewer delete err = %v, want ErrCalendarReadOnly", err)
	}
}

// TestEventService_Stranger_GetsNotFoundNotForbidden covers "A User with no
// Access cannot see or address the Calendar at all, and gets not-found
// rather than forbidden" (#100).
func TestEventService_Stranger_GetsNotFoundNotForbidden(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := f.events.Get(ctx, f.strangerID, event.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("stranger get event err = %v, want ErrNotFound", err)
	}
	if _, err := f.calendars.Get(ctx, f.strangerID, f.calendarID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("stranger get calendar err = %v, want ErrNotFound", err)
	}

	events, err := f.events.List(ctx, f.strangerID, nil, nil)
	if err != nil {
		t.Fatalf("stranger list: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events visible to a stranger, got %d", len(events))
	}
}

// TestEventService_RevokeShare_RemovesCalendarFromList covers "Alice
// revokes the Share; Family disappears from Bob's Calendar list entirely"
// (#100).
func TestEventService_RevokeShare_RemovesCalendarFromList(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	if err := f.calendars.RevokeShare(ctx, f.ownerID, f.calendarID, f.editorID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	list, err := f.calendars.ListAccessible(ctx, f.editorID)
	if err != nil {
		t.Fatalf("list accessible: %v", err)
	}
	for _, c := range list {
		if c.ID == f.calendarID {
			t.Fatalf("revoked calendar %q still present in editor's list", f.calendarID)
		}
	}

	if _, err := f.events.Create(ctx, f.editorID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Nope", Start: shareTestStart, End: shareTestEnd}); !errors.Is(err, ErrCalendarNotFound) {
		t.Fatalf("create after revoke err = %v, want ErrCalendarNotFound", err)
	}
}

// TestEventService_RoleChange_TakesEffectImmediately covers "Alice changes
// his Role to Viewer; his next write is refused" (#100) — a Role change is
// resolved at Access time, not cached from grant time.
func TestEventService_RoleChange_TakesEffectImmediately(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.editorID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd})
	if err != nil {
		t.Fatalf("editor create: %v", err)
	}

	if _, _, err := f.calendars.Share(ctx, f.ownerID, f.calendarID, "editor@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("demote to viewer: %v", err)
	}

	if _, err := f.events.Update(ctx, f.editorID, event.ID, EventWrite{CalendarID: f.calendarID, Title: "Nope", Start: shareTestStart, End: shareTestEnd}); !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("demoted editor's update err = %v, want ErrCalendarReadOnly", err)
	}
}

// TestEventService_SubscriptionClampsEditorToo covers "Attaching a
// Subscription to an already-shared Calendar causes an existing Editor's
// writes to be refused" (#100, ADR-0034) — the clamp is resolved fresh
// against the Calendar's current source_url every time, not decided once
// when the Share was granted.
func TestEventService_SubscriptionClampsEditorToo(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	if _, err := f.calendars.UpdateSourceURL(ctx, f.ownerID, f.calendarID, "https://example.com/feed.ics"); err != nil {
		t.Fatalf("attach subscription: %v", err)
	}

	if _, err := f.events.Create(ctx, f.editorID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Nope", Start: shareTestStart, End: shareTestEnd}); !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("editor create on subscribed calendar err = %v, want ErrCalendarReadOnly", err)
	}

	// Owner is clamped too — a Subscription is read-only for everyone
	// (ADR-0032), including whoever attached it.
	if _, err := f.events.Create(ctx, f.ownerID, "evt-2", EventWrite{CalendarID: f.calendarID, Title: "Nope", Start: shareTestStart, End: shareTestEnd}); !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("owner create on subscribed calendar err = %v, want ErrCalendarReadOnly", err)
	}
}
