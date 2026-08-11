// event_attendee_count_test.go covers #193's AttendeeCount: an Event's
// number of Attendees, attached by the service layer (mirroring
// CreatedByName/CalendarColor) so a caller deciding whether an Event "has
// Attendees" has a synchronous answer at List/Get/Create/Update time,
// without a separate round-trip to the Attendees endpoint.
package service

import (
	"context"
	"testing"
	"time"
)

func TestEventService_Create_AttendeeCountReflectsInvitedAttendees(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeUserIDs: []int64{f.member1ID, f.member2ID},
	}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if event.AttendeeCount != 2 {
		t.Fatalf("expected AttendeeCount 2, got %d", event.AttendeeCount)
	}
}

func TestEventService_Create_AttendeeCountZeroWithNoAttendees(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if event.AttendeeCount != 0 {
		t.Fatalf("expected AttendeeCount 0, got %d", event.AttendeeCount)
	}
}

func TestEventService_Get_AttendeeCountReflectsInvitedAttendees(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	created, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeUserIDs: []int64{f.member1ID},
	}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	fetched, err := f.events.Get(ctx, f.ownerID, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.AttendeeCount != 1 {
		t.Fatalf("expected AttendeeCount 1, got %d", fetched.AttendeeCount)
	}
}

func TestEventService_List_AttendeeCountReflectsInvitedAttendees(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	created, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeUserIDs: []int64{f.member1ID, f.member2ID},
	}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	events, err := f.events.List(ctx, f.ownerID, nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, e := range events {
		if e.ID == created.ID {
			found = true
			if e.AttendeeCount != 2 {
				t.Fatalf("expected AttendeeCount 2, got %d", e.AttendeeCount)
			}
		}
	}
	if !found {
		t.Fatalf("expected to find event %q in list", created.ID)
	}
}

func TestEventService_Update_PreservesAttendeeCount(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	created, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeUserIDs: []int64{f.member1ID},
	}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := f.events.Update(ctx, f.ownerID, created.ID, EventWrite{
		CalendarID: f.calendarID,
		Title:      "Discuss tech stack (updated)",
		Start:      created.Start,
		End:        created.End,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.AttendeeCount != 1 {
		t.Fatalf("expected AttendeeCount 1, got %d", updated.AttendeeCount)
	}
}
