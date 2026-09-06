// connection_picker.go implements the Calendar picker (#286, ADR-0052): the
// step after authorizing where a User chooses which of the account's
// calendars come in. ListCalendars is the picker's read side — everything
// the account can see, with Google's own selected flag and accessRole
// surfaced for the picker to render — and ImportCalendars is its write side,
// turning the chosen rows into Linked Calendars in the caller's active
// Workspace.
package service

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/repository"
)

// ErrConnectionCalendarsUnavailable is returned by ListCalendars/
// ImportCalendars when connectionID carries no access token yet to call
// Google with — unreachable in practice, since Callback always populates one
// on the same request that creates the Connection, but guarded rather than
// dereferenced blindly.
var ErrConnectionCalendarsUnavailable = errors.New("this connection has no usable google access token")

// PickerCalendar is one row the Calendar picker offers (#286): everything
// the connected account can see at Google — its own calendars, the ones it
// subscribed to, and the ones other people shared to it.
type PickerCalendar struct {
	// ExternalID is the Provider's own id for this calendar — what
	// ImportCalendars matches the caller's selection against, and what a
	// picked row's Linked Calendar stores as its Source's
	// ExternalCalendarID.
	ExternalID string
	Name       string
	Color      string
	// Selected mirrors Google's own sidebar checkbox — the default checked
	// state the picker's UI pre-fills from (#286's acceptance criteria: "the
	// Provider's own selection flag drives which rows are pre-checked").
	Selected bool
	// Writable reports whether Google's own ACL lets this account write to
	// the calendar there. Rendered as the picker's read-only badge, and what
	// ImportCalendars derives the created Source's Mode from (#290,
	// ADR-0075) — re-read from Google again on every later Refresh rather
	// than fixed from this one snapshot.
	Writable bool
}

func toPickerCalendars(entries []googleCalendarListEntry) []PickerCalendar {
	calendars := make([]PickerCalendar, len(entries))
	for i, e := range entries {
		calendars[i] = toPickerCalendar(e, i)
	}
	return calendars
}

// toPickerCalendar maps one Google calendarList entry to a PickerCalendar.
// index feeds importColorRotation's fallback for a calendar with no
// BackgroundColor Google sends (rare, but not guaranteed) — mirroring
// SubscribeService's own proposeNameColor.
func toPickerCalendar(e googleCalendarListEntry, index int) PickerCalendar {
	name := e.displayName()
	if name == "" {
		// A calendar with no Summary at all would otherwise fail
		// CalendarService's name validation on import for a reason the User
		// never typed — the Provider's own id is at least stable and
		// visible, unlike a blank string (#286).
		name = e.ID
	}

	color, ok := NormalizeColor(e.BackgroundColor)
	if !ok {
		color = importColorRotation[index%len(importColorRotation)]
	}

	return PickerCalendar{
		ExternalID: e.ID,
		Name:       name,
		Color:      color,
		Selected:   e.Selected,
		Writable:   e.writable(),
	}
}

// accessTokenFor returns connectionID's usable access token, or
// ErrConnectionNotFound/ErrConnectionCalendarsUnavailable.
func (s *ConnectionService) accessTokenFor(ctx context.Context, userID, connectionID int64) (repository.Connection, string, error) {
	conn, err := s.connections.GetByID(ctx, userID, connectionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.Connection{}, "", ErrConnectionNotFound
		}
		return repository.Connection{}, "", fmt.Errorf("get connection: %w", err)
	}
	if conn.AccessToken == nil || *conn.AccessToken == "" {
		return repository.Connection{}, "", ErrConnectionCalendarsUnavailable
	}
	return conn, *conn.AccessToken, nil
}

// ListCalendars returns every calendar connectionID's account can see (#286)
// — the picker's read side, called right after Connect/Callback while the
// access token Callback just minted is still fresh.
func (s *ConnectionService) ListCalendars(ctx context.Context, userID, connectionID int64) ([]PickerCalendar, error) {
	if !s.configured {
		return nil, ErrGoogleNotConfigured
	}

	_, accessToken, err := s.accessTokenFor(ctx, userID, connectionID)
	if err != nil {
		return nil, err
	}

	entries, err := s.google.listCalendarList(ctx, accessToken)
	if err != nil {
		return nil, err
	}

	return toPickerCalendars(entries), nil
}

// ImportCalendars creates a Linked Calendar in workspaceID for every one of
// externalIDs that connectionID's account still carries, ignoring any id
// that no longer matches — the picker showed a snapshot, and Google's own
// listing is re-fetched here rather than trusted from the picker's own
// response, so a User's confirmed selection can only ever import calendars
// the account can currently see, never ones a client happened to send.
//
// Each Source's Mode is derived from Google's own AccessRole at import time
// (#290, ADR-0075): writable when the connected account can write to the
// calendar there, read-only when it's merely shared in, or one of the
// Provider's own generated Holidays/Birthdays calendars. Re-derived on every
// Refresh (connection_refresh.go), never fixed here for the Calendar's
// lifetime — an ACL change at the Provider is exactly the kind of thing a
// Refresh cycle is what notices.
//
// A failure partway through leaves whichever calendars already succeeded in
// place, mirroring calling Subscribe once per selected calendar: each one is
// an independent Calendar+Source pair (CalendarService.CreateSubscribed),
// so there is no larger unit to roll back to.
//
// Each newly created Linked Calendar's first Full Refresh (#287) runs here,
// synchronously, before ImportCalendars returns — the same shape Subscribe
// already uses for a feed's initial import, so the picker's own "Confirm"
// button (already awaited, already showing a loading state) is what makes
// the fetch's pagination visibly take time rather than silently happening
// later. A Full Refresh failing does not undo the Calendar it belongs to
// (ADR-0033: failure never deletes anything) — it is recorded as the
// Source's own needs-attention/retrying state, discoverable the same way a
// Subscription's failed Refresh already is, and logged here since nothing
// in this ticket surfaces a per-import summary of its own yet.
func (s *ConnectionService) ImportCalendars(ctx context.Context, userID, workspaceID, connectionID int64, externalIDs []string) ([]repository.Calendar, error) {
	if !s.configured {
		return nil, ErrGoogleNotConfigured
	}

	conn, accessToken, err := s.accessTokenFor(ctx, userID, connectionID)
	if err != nil {
		return nil, err
	}

	entries, err := s.google.listCalendarList(ctx, accessToken)
	if err != nil {
		return nil, err
	}

	wanted := make(map[string]bool, len(externalIDs))
	for _, id := range externalIDs {
		wanted[id] = true
	}

	var created []repository.Calendar
	for i, e := range entries {
		if !wanted[e.ID] {
			continue
		}
		picker := toPickerCalendar(e, i)
		externalCalendarID := picker.ExternalID

		mode := repository.SourceModeReadOnly
		if picker.Writable {
			mode = repository.SourceModeWritable
		}
		calendar, err := s.calendars.CreateSubscribed(ctx, userID, workspaceID, uuid.NewString(), CalendarWrite{
			Name:  picker.Name,
			Color: picker.Color,
		}, repository.SourceFields{
			Kind:               repository.SourceKindConnection,
			Mode:               mode,
			ConnectionID:       &conn.ID,
			ExternalCalendarID: &externalCalendarID,
		})
		if err != nil {
			return created, fmt.Errorf("import calendar %q: %w", picker.Name, err)
		}
		created = append(created, calendar)

		s.runInitialFullRefresh(ctx, userID, calendar)
	}

	return created, nil
}

// runInitialFullRefresh drives calendar's first Full Refresh right after
// ImportCalendars creates it, logging rather than failing the import on
// error or on an unmapped recurrence feature — see ImportCalendars' own doc
// comment for why.
func (s *ConnectionService) runInitialFullRefresh(ctx context.Context, userID int64, calendar repository.Calendar) {
	result, err := s.FullRefresh(ctx, userID, calendar.ID)
	if err != nil {
		log.Printf("linked calendar initial full refresh (calendar=%s): %v", calendar.ID, err)
		return
	}
	if result.DroppedRecurrenceLines > 0 || result.Unparseable > 0 {
		log.Printf("linked calendar initial full refresh (calendar=%s): %d unsupported recurrence line(s) dropped, %d series unmappable",
			calendar.ID, result.DroppedRecurrenceLines, result.Unparseable)
	}
}
