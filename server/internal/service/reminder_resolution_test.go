package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/repository"
)

// newTestReminderResolutionFixture returns an EventService plus an owner and
// a member sharing one Calendar (member has Editor Access via a Share) —
// the two-User setup ADR-0064's Default reminders resolution tests need.
func newTestReminderResolutionFixture(t *testing.T) (svc *EventService, ownerID, memberID int64, calendarID string) {
	t.Helper()

	g := newTestGraph(t)

	ctx := context.Background()
	users := g.UserRepo
	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := users.Create(ctx, "member", "member@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	workspaceRepo := g.WorkspaceRepo
	workspace, err := workspaceRepo.Create(ctx, "Test Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, owner.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add owner member: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, member.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add plain member: %v", err)
	}

	calendarRepo := g.CalendarRepo
	cal, err := calendarRepo.Create(ctx, owner.ID, workspace.ID, "cal-1", repository.CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	calendarService := g.Calendars
	if _, _, err := calendarService.Share(ctx, owner.ID, cal.ID, member.Email, repository.RoleEditor); err != nil {
		t.Fatalf("share calendar with member: %v", err)
	}

	svc = g.Events

	return svc, owner.ID, member.ID, cal.ID
}

func TestGetReminders_ResolvesCalendarDefaultWhenNoneOfOwn(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 30, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	reminders, err := svc.GetReminders(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get reminders: %v", err)
	}
	if len(reminders) != 1 || reminders[0].OffsetMinutes != 30 || reminders[0].DefaultReminderID == 0 {
		t.Fatalf("expected one resolved default reminder, got %+v", reminders)
	}

	// The owner set no default of their own, so they get nothing.
	ownerReminders, err := svc.GetReminders(ctx, ownerID, event.ID)
	if err != nil {
		t.Fatalf("get owner reminders: %v", err)
	}
	if len(ownerReminders) != 0 {
		t.Fatalf("expected owner to have no reminders, got %+v", ownerReminders)
	}
}

func TestSetReminders_ExplicitEmptyOptsOutOfDefault(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 30, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	// member explicitly saves an empty list — "no Reminders on this one
	// Event" (ADR-0064) — which must survive as explicit-empty, not fall
	// back to their default.
	if _, err := svc.SetReminders(ctx, memberID, event.ID, nil); err != nil {
		t.Fatalf("set reminders empty: %v", err)
	}

	reminders, err := svc.GetReminders(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get reminders: %v", err)
	}
	if len(reminders) != 0 {
		t.Fatalf("expected explicit-empty to suppress the default, got %+v", reminders)
	}
}

func TestSetReminders_ExplicitNonEmptyOverridesDefault(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 30, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	if _, err := svc.SetReminders(ctx, memberID, event.ID, []repository.Reminder{{OffsetMinutes: 5, Channel: "email"}}); err != nil {
		t.Fatalf("set reminders: %v", err)
	}

	reminders, err := svc.GetReminders(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get reminders: %v", err)
	}
	if len(reminders) != 1 || reminders[0].OffsetMinutes != 5 || reminders[0].Channel != "email" || reminders[0].DefaultReminderID != 0 {
		t.Fatalf("expected the explicit row alone, got %+v", reminders)
	}
}

func TestGetReminders_AllDayEventResolvesAllDayDefault(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("set timed default: %v", err)
	}
	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, true, []repository.Reminder{{OffsetMinutes: 540, Channel: "notification"}}); err != nil {
		t.Fatalf("set all-day default: %v", err)
	}

	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Bin day", Start: day, End: day.AddDate(0, 0, 1), AllDay: true})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	reminders, err := svc.GetReminders(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get reminders: %v", err)
	}
	if len(reminders) != 1 || reminders[0].OffsetMinutes != 540 {
		t.Fatalf("expected the all-day default (540), got %+v", reminders)
	}
}

func TestCreate_ResolvesCreatorsOwnDefault(t *testing.T) {
	svc, ownerID, _, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, ownerID, calendarID, false, []repository.Reminder{{OffsetMinutes: 15, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	if len(event.Reminders) != 1 || event.Reminders[0].OffsetMinutes != 15 {
		t.Fatalf("expected the creator's default pre-resolved on create, got %+v", event.Reminders)
	}
}

// The one thing #216 buys: for a given (User, Event), what the grid shows and
// what the scheduler fires on come from one resolution, so they cannot drift.
// Each case exercises one arm of ADR-0064's rule.
func TestResolution_ViewerReadAndFiringReadAgree(t *testing.T) {
	timed := []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}
	allDay := []repository.Reminder{{OffsetMinutes: 540, Channel: "notification"}}

	cases := []struct {
		name string
		// event's kind decides which of the two Default lists can resolve
		// onto it — they are separately useless to each other (ADR-0064).
		eventIsAllDay bool
		set           func(t *testing.T, svc *EventService, memberID int64, calendarID, eventID string)
		wantOffset    int
		wantIsDefault bool
		wantNone      bool
	}{
		{
			name: "their own rows",
			set: func(t *testing.T, svc *EventService, memberID int64, calendarID, eventID string) {
				setDefaults(t, svc, memberID, calendarID, timed, nil)
				if _, err := svc.SetReminders(context.Background(), memberID, eventID, []repository.Reminder{{OffsetMinutes: 5, Channel: "email"}}); err != nil {
					t.Fatalf("set own reminders: %v", err)
				}
			},
			wantOffset: 5,
		},
		{
			name: "their explicit-empty marker",
			set: func(t *testing.T, svc *EventService, memberID int64, calendarID, eventID string) {
				setDefaults(t, svc, memberID, calendarID, timed, nil)
				if _, err := svc.SetReminders(context.Background(), memberID, eventID, nil); err != nil {
					t.Fatalf("opt out: %v", err)
				}
			},
			wantNone: true,
		},
		{
			name: "their Calendar's timed default",
			set: func(t *testing.T, svc *EventService, memberID int64, calendarID, eventID string) {
				setDefaults(t, svc, memberID, calendarID, timed, allDay)
			},
			wantOffset:    10,
			wantIsDefault: true,
		},
		{
			name:          "their Calendar's all-day default",
			eventIsAllDay: true,
			set: func(t *testing.T, svc *EventService, memberID int64, calendarID, eventID string) {
				setDefaults(t, svc, memberID, calendarID, timed, allDay)
			},
			wantOffset:    540,
			wantIsDefault: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
			ctx := context.Background()

			write := EventWrite{CalendarID: calendarID, Title: "Bin day", Start: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)}
			if tc.eventIsAllDay {
				day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				write.Start, write.End, write.AllDay = day, day.AddDate(0, 0, 1), true
			}
			event, err := svc.Create(ctx, ownerID, "evt-1", write)
			if err != nil {
				t.Fatalf("create event: %v", err)
			}
			tc.set(t, svc, memberID, calendarID, event.ID)

			viewer, err := svc.GetReminders(ctx, memberID, event.ID)
			if err != nil {
				t.Fatalf("get reminders: %v", err)
			}
			_, resolved, err := svc.ListAllWithReminders(resolutionWindow(ctx))
			if err != nil {
				t.Fatalf("list all with reminders: %v", err)
			}
			firing := resolved.For(event.ID, memberID)

			if !reflect.DeepEqual(viewer, firing) {
				t.Fatalf("the viewer read and the firing read disagree: viewer=%+v firing=%+v", viewer, firing)
			}

			// ...and they agree on the right answer, not vacuously on nothing.
			if tc.wantNone {
				if len(viewer) != 0 {
					t.Fatalf("expected no Reminders to resolve, got %+v", viewer)
				}
				return
			}
			if len(viewer) != 1 || viewer[0].OffsetMinutes != tc.wantOffset {
				t.Fatalf("expected one Reminder at offset %d, got %+v", tc.wantOffset, viewer)
			}
			if isDefault := viewer[0].DefaultReminderID != 0; isDefault != tc.wantIsDefault {
				t.Fatalf("expected DefaultReminderID set=%v, got %+v", tc.wantIsDefault, viewer[0])
			}
		})
	}
}

// setDefaults writes memberID's two Default reminder lists on calendarID —
// either may be nil, meaning "no default of that kind".
func setDefaults(t *testing.T, svc *EventService, memberID int64, calendarID string, timed, allDay []repository.Reminder) {
	t.Helper()

	ctx := context.Background()
	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, timed); err != nil {
		t.Fatalf("set timed default: %v", err)
	}
	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, true, allDay); err != nil {
		t.Fatalf("set all-day default: %v", err)
	}
}

func TestListAllWithReminders_ResolvesDefaultsForFiring(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	events, resolved, err := svc.ListAllWithReminders(resolutionWindow(ctx))
	if err != nil {
		t.Fatalf("list all with reminders: %v", err)
	}

	if !containsEventID(events, event.ID) {
		t.Fatalf("expected event %s to carry a resolved default, events=%+v", event.ID, events)
	}
	memberReminders := resolved.For(event.ID, memberID)
	if len(memberReminders) != 1 || memberReminders[0].OffsetMinutes != 10 || memberReminders[0].DefaultReminderID == 0 {
		t.Fatalf("expected member's resolved default, got %+v", memberReminders)
	}
	if len(resolved.For(event.ID, ownerID)) != 0 {
		t.Fatalf("owner set no default and no explicit rows, expected none, got %+v", resolved.For(event.ID, ownerID))
	}
}

// resolutionWindow is a firing window comfortably around the 2026-01-01 Events
// every resolution test here creates: these are about which Reminders resolve,
// not about the window that bounds the read (#219), which has its own tests.
func resolutionWindow(ctx context.Context) (context.Context, time.Time, time.Time) {
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return ctx, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1)
}

// containsEventID reports whether the firing read returned eventID at all — an
// Event resolving nothing for anybody is dropped before it reaches the engine.
func containsEventID(events []repository.Event, eventID string) bool {
	for _, e := range events {
		if e.ID == eventID {
			return true
		}
	}
	return false
}

func TestListAllWithReminders_ExplicitEmptyOptsOutOfFiring(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := svc.SetReminders(ctx, memberID, event.ID, nil); err != nil {
		t.Fatalf("opt out: %v", err)
	}

	_, resolved, err := svc.ListAllWithReminders(resolutionWindow(ctx))
	if err != nil {
		t.Fatalf("list all with reminders: %v", err)
	}
	if len(resolved.For(event.ID, memberID)) != 0 {
		t.Fatalf("expected member's explicit opt-out to suppress firing, got %+v", resolved.For(event.ID, memberID))
	}
}

// TestGetSeries_ResolvesDefaults confirms the CalDAV/ICS-export read path
// (GetSeries -> hydrateSeries -> attachResolvedViewerReminders) now serves
// the same resolved set the live web-app path does — a default that only
// resolves appears as a VALARM there too (#213).
func TestGetSeries_ResolvesDefaults(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	master, _, err := svc.GetSeries(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get series: %v", err)
	}
	if len(master.Reminders) != 1 || master.Reminders[0].OffsetMinutes != 10 || master.Reminders[0].DefaultReminderID == 0 {
		t.Fatalf("expected GetSeries to resolve the default, got %+v", master.Reminders)
	}

	// The live web-app path (Get) resolves the very same set.
	webEvent, err := svc.Get(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(webEvent.Reminders) != 1 {
		t.Fatalf("expected Get to resolve the default, got %+v", webEvent.Reminders)
	}

	// An explicit empty list still suppresses the default in both paths
	// alike (AC3): no VALARM appears where a User opted out.
	if _, err := svc.SetReminders(ctx, memberID, event.ID, nil); err != nil {
		t.Fatalf("opt out: %v", err)
	}
	master, _, err = svc.GetSeries(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get series after opt-out: %v", err)
	}
	if len(master.Reminders) != 0 {
		t.Fatalf("expected explicit-empty to suppress the default in GetSeries too, got %+v", master.Reminders)
	}
}

// TestPutSeries_EmptyRemindersMarksExplicit confirms a CalDAV PUT clearing a
// User's VALARMs (an empty Reminders list) marks the same explicit-empty
// opt-out the web app's dedicated SetReminders does (ADR-0064) — "A PUT's
// VALARMs write the PUTting User's Reminders" means the Calendar default
// must not silently reassert on the next resolved (web-app) read.
func TestPutSeries_EmptyRemindersMarksExplicit(t *testing.T) {
	svc, ownerID, _, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, ownerID, calendarID, false, []repository.Reminder{{OffsetMinutes: 30, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	master, _, err := svc.PutSeries(ctx, ownerID, calendarID, "evt-1", SeriesWrite{
		Title: "Standup", Start: start, End: start.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("put series: %v", err)
	}

	reminders, err := svc.GetReminders(ctx, ownerID, master.ID)
	if err != nil {
		t.Fatalf("get reminders: %v", err)
	}
	if len(reminders) != 0 {
		t.Fatalf("expected the PUT's empty VALARM set to opt out of the default, got %+v", reminders)
	}
}

// TestListSeriesByCalendar_ResolvesDefaults confirms the ICS-export/CalDAV
// collection-listing read path (ListSeriesByCalendar -> hydrateEvents with
// seriesEventFields) resolves Calendar defaults too, not just the
// single-object GetSeries path (#213).
func TestListSeriesByCalendar_ResolvesDefaults(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 20, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	masters, _, err := svc.ListSeriesByCalendar(ctx, memberID, calendarID)
	if err != nil {
		t.Fatalf("list series by calendar: %v", err)
	}

	var found *repository.Event
	for i := range masters {
		if masters[i].ID == event.ID {
			found = &masters[i]
		}
	}
	if found == nil {
		t.Fatalf("expected event %s in listing, got %+v", event.ID, masters)
	}
	if len(found.Reminders) != 1 || found.Reminders[0].OffsetMinutes != 20 {
		t.Fatalf("expected ListSeriesByCalendar to resolve the member's default, got %+v", found.Reminders)
	}
}

// TestSetDefaultReminders_BumpsCalendarCTag confirms changing a Calendar
// default bumps the Calendar's CTag (AC4, #213), so a device holding a
// sync-token notices even though no Event row was itself written.
func TestSetDefaultReminders_BumpsCalendarCTag(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	if _, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	before, err := svc.CalendarCTag(ctx, memberID, calendarID)
	if err != nil {
		t.Fatalf("ctag before: %v", err)
	}

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 30, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}
	if err := svc.BumpCalendarChangeSeq(ctx, memberID, calendarID); err != nil {
		t.Fatalf("bump calendar change seq: %v", err)
	}

	after, err := svc.CalendarCTag(ctx, memberID, calendarID)
	if err != nil {
		t.Fatalf("ctag after: %v", err)
	}
	if after <= before {
		t.Fatalf("expected ctag to increase after a default reminders change, before=%d after=%d", before, after)
	}
}

func TestRevokeShare_ClearsDefaultRemindersAndExplicitMarkers(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := svc.SetReminders(ctx, memberID, event.ID, nil); err != nil {
		t.Fatalf("opt out: %v", err)
	}

	if err := svc.calendars.RevokeShare(ctx, ownerID, calendarID, memberID); err != nil {
		t.Fatalf("revoke share: %v", err)
	}

	timed, allDay, err := svc.calendars.GetDefaultReminders(ctx, memberID, calendarID)
	if err == nil {
		t.Fatalf("expected member's default reminders lookup to fail once they've lost Access, got timed=%+v allDay=%+v", timed, allDay)
	}

	// Re-share and confirm the default is gone (not silently reactivated)
	// and the old explicit-empty marker no longer suppresses a fresh
	// default — the member simply has neither anymore.
	if _, _, err := svc.calendars.Share(ctx, ownerID, calendarID, "member@example.com", repository.RoleEditor); err != nil {
		t.Fatalf("re-share: %v", err)
	}
	reminders, err := svc.GetReminders(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get reminders after re-share: %v", err)
	}
	if len(reminders) != 0 {
		t.Fatalf("expected no reminders after re-share (default was cleared), got %+v", reminders)
	}
}
