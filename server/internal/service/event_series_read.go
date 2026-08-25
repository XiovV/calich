package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/XiovV/calich/server/internal/recurrence"
	"github.com/XiovV/calich/server/internal/repository"
)

// GetSeries returns masterID's Master (with Exdates and Reminders attached)
// and its Overrides (each with its own Reminders attached), for CalDAV's
// {masterId}.ics resource (ADR-0025). Only a Master is independently
// addressable, so masterID naming an Override returns ErrParentIsOverride.
func (s *EventService) GetSeries(ctx context.Context, userID int64, masterID string) (repository.Event, []repository.Event, error) {
	master, err := s.getOwnedEvent(ctx, userID, masterID)
	if err != nil {
		return repository.Event{}, nil, err
	}
	if master.ParentID != nil {
		return repository.Event{}, nil, ErrParentIsOverride
	}

	overridesByParent, err := s.events.ListChildrenByParentIDs(ctx, []string{masterID})
	if err != nil {
		return repository.Event{}, nil, fmt.Errorf("list overrides: %w", err)
	}

	return s.hydrateSeries(ctx, userID, master, overridesByParent[masterID])
}

// hydrateSeries hydrates master and overrides together as one series,
// returning master then overrides — the tail GetSeries and
// GetAttendeeOnlySeries share once each has resolved masterID and confirmed
// the caller's own visibility to it (Calendar Access, or Attendee —
// ADR-0046). userID is whoever is asking, so Reminders resolve to their own
// rows, not the Calendar's Owner's (ADR-0064).
func (s *EventService) hydrateSeries(ctx context.Context, userID int64, master repository.Event, overrides []repository.Event) (repository.Event, []repository.Event, error) {
	all := append([]repository.Event{master}, overrides...)
	if err := s.hydrateEvents(ctx, eventPointers(all), userID, seriesEventFields); err != nil {
		return repository.Event{}, nil, err
	}
	return all[0], all[1:], nil
}

// GetSeriesForEvent is GetSeries generalized to accept id naming either a
// Master or one of its Overrides — both resolve to the same series, since a
// caller addressing an Occurrence often only knows the Override id it fired
// against, not its parent's (#76).
func (s *EventService) GetSeriesForEvent(ctx context.Context, userID int64, id string) (repository.Event, []repository.Event, error) {
	event, err := s.getOwnedEvent(ctx, userID, id)
	if err != nil {
		return repository.Event{}, nil, err
	}

	masterID := id
	if event.ParentID != nil {
		masterID = *event.ParentID
	}
	return s.GetSeries(ctx, userID, masterID)
}

// GetOccurrence returns id's series flattened to the single Occurrence
// starting at occurrenceStart, for ICS export's scope=occurrence (#76): the
// matching Override's own fields verbatim if one exists at that
// RecurrenceID, or the Master's own fields with Start/End shifted to
// occurrenceStart/occurrenceStart+duration otherwise, and the series'
// Attachments either way. id may name either a Master or an Override (see
// GetSeriesForEvent). The returned Event always carries a cleared
// Rrule/ParentID/RecurrenceID — it describes one concrete Occurrence, not a
// series or a series member.
func (s *EventService) GetOccurrence(ctx context.Context, userID int64, id string, occurrenceStart time.Time) (repository.Event, error) {
	master, overrides, err := s.GetSeriesForEvent(ctx, userID, id)
	if err != nil {
		return repository.Event{}, err
	}

	if override, ok := findOverrideForOccurrence(overrides, occurrenceStart); ok {
		flattened := override
		flattened.ParentID = nil
		flattened.RecurrenceID = nil
		// An Attachment hangs off the Master, so only the Master's row
		// carries any (hydrateEvents' attachAttachments). Every Occurrence
		// shows the series' Attachments (ADR-0040) and every export scope
		// inlines them (#217), so an overridden Occurrence carries them too
		// — without this it would export as the one Occurrence that has none.
		flattened.Attachments = master.Attachments
		// An Override never carries a rule of its own (ADR-0016), so
		// this is always already "", but cleared explicitly since
		// icalendar.OccurrenceToICal trusts its caller to have done so.
		flattened.Rrule = ""
		return flattened, nil
	}

	valid, err := isOccurrenceStart(master, occurrenceStart)
	if err != nil {
		return repository.Event{}, err
	}
	if !valid {
		return repository.Event{}, ErrOccurrenceNotFound
	}

	flattened := master
	flattened.Start = occurrenceStart
	flattened.End = occurrenceStart.Add(master.End.Sub(master.Start))
	flattened.Rrule = ""
	flattened.Exdates = nil
	return flattened, nil
}

// findOverrideForOccurrence returns the Override in overrides that replaces
// occurrenceStart, if any — the "does this Occurrence have an Override"
// lookup GetOccurrence and resolveReplyEvent both need, kept in one place so
// the two don't drift apart on what counts as a match.
func findOverrideForOccurrence(overrides []repository.Event, occurrenceStart time.Time) (repository.Event, bool) {
	for _, override := range overrides {
		if override.RecurrenceID != nil && override.RecurrenceID.Equal(occurrenceStart) {
			return override, true
		}
	}
	return repository.Event{}, false
}

// isOccurrenceStart reports whether occurrenceStart is a real, non-excepted
// Occurrence of master's series: master's own start if it doesn't recur, or
// one of the rrule's generated starts otherwise — reusing the same Go
// expander CalDAV's time-range query uses (recurrence.ExpandOccurrences)
// rather than adding a third implementation (see the note on #74).
func isOccurrenceStart(master repository.Event, occurrenceStart time.Time) (bool, error) {
	for _, exdate := range master.Exdates {
		if exdate.Equal(occurrenceStart) {
			return false, nil
		}
	}

	if master.Rrule == "" {
		return master.Start.Equal(occurrenceStart), nil
	}

	starts, err := recurrence.ExpandOccurrences(master.Rrule, master.Tzid, master.Start, occurrenceStart, occurrenceStart.Add(time.Second))
	if err != nil {
		return false, fmt.Errorf("expand occurrences: %w", err)
	}
	for _, start := range starts {
		if start.Equal(occurrenceStart) {
			return true, nil
		}
	}
	return false, nil
}

// ListSeriesByCalendar returns every Master in calendarID (with Exdates and
// Reminders attached) alongside a parentID-keyed map of each Master's
// Overrides (each with its own Reminders attached) — CalDAV's per-calendar
// object listing (ADR-0025).
func (s *EventService) ListSeriesByCalendar(ctx context.Context, userID int64, calendarID string) ([]repository.Event, map[string][]repository.Event, error) {
	_, err := s.calendarByID(ctx, userID, calendarID)
	if err != nil {
		if errors.Is(err, ErrCalendarNotFound) {
			return []repository.Event{}, map[string][]repository.Event{}, nil
		}
		return nil, nil, err
	}

	masters, err := s.events.ListMastersByCalendar(ctx, calendarID)
	if err != nil {
		return nil, nil, fmt.Errorf("list masters: %w", err)
	}

	overridesByParent, rows, err := s.loadSeriesOverrides(ctx, masters)
	if err != nil {
		return nil, nil, err
	}
	if err := s.hydrateEvents(ctx, rows, userID, seriesEventFields); err != nil {
		return nil, nil, err
	}
	return masters, overridesByParent, nil
}

// ListStoredReminders returns userID's own Reminders on eventIDs exactly as
// stored — the raw event_reminders rows, with no Calendar-default
// resolution layered on top (ADR-0064). Refresh's diff needs this rather
// than a viewer's resolved set (as ListSeriesByCalendar's Reminders are):
// a Default reminder is the Owner's own state beside the Calendar, never
// content a feed could send, so comparing against it can never converge
// (#220).
func (s *EventService) ListStoredReminders(ctx context.Context, userID int64, eventIDs []string) (map[string][]repository.Reminder, error) {
	byEvent, err := s.reminders.ListByEventIDs(ctx, eventIDs, []int64{userID})
	if err != nil {
		return nil, fmt.Errorf("list reminders: %w", err)
	}
	stored := make(map[string][]repository.Reminder, len(byEvent))
	for eventID, byUser := range byEvent {
		stored[eventID] = byUser[userID]
	}
	return stored, nil
}

// ListAttendeeOnlySeries returns, shaped like ListSeriesByCalendar, every
// series userID is an Attendee of (on its Master or any of its Overrides)
// whose Calendar userID has no Access to (ADR-0046, #163) — the content of
// the CalDAV backend's synthetic Attendee-only collection, which no real
// per-principal Calendar collection backs for userID. A series userID both
// has Calendar Access to and is an Attendee of is excluded here: it already
// appears once, correctly attributed, under that Calendar's own collection
// (ListSeriesByCalendar), and must not be listed twice.
func (s *EventService) ListAttendeeOnlySeries(ctx context.Context, userID int64) ([]repository.Event, map[string][]repository.Event, error) {
	accessible, err := s.calendars.ListAccessible(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("list calendars: %w", err)
	}
	accessibleIDs := make(map[string]bool, len(accessible))
	for _, c := range accessible {
		accessibleIDs[c.ID] = true
	}

	attendeeEvents, err := s.events.ListByAttendeeUserID(ctx, userID, nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("list attendee events: %w", err)
	}

	masterIDs := []string{}
	seenMasterID := map[string]bool{}
	for _, e := range attendeeEvents {
		if accessibleIDs[e.CalendarID] {
			continue
		}
		masterID := e.ID
		if e.ParentID != nil {
			masterID = *e.ParentID
		}
		if seenMasterID[masterID] {
			continue
		}
		seenMasterID[masterID] = true
		masterIDs = append(masterIDs, masterID)
	}

	masters := make([]repository.Event, 0, len(masterIDs))
	for _, id := range masterIDs {
		master, err := s.events.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				continue
			}
			return nil, nil, fmt.Errorf("get master event: %w", err)
		}
		masters = append(masters, master)
	}

	overridesByParent, rows, err := s.loadSeriesOverrides(ctx, masters)
	if err != nil {
		return nil, nil, err
	}
	if err := s.hydrateEvents(ctx, rows, userID, seriesEventFields); err != nil {
		return nil, nil, err
	}
	return masters, overridesByParent, nil
}

// isAttendeeOfEventIDs reports whether userID is an Attendee of any of ids —
// GetAttendeeOnlySeries' visibility check, run across a Master and its
// Overrides since an Attendee invite can name either (#161).
func (s *EventService) isAttendeeOfEventIDs(ctx context.Context, userID int64, ids []string) (bool, error) {
	for _, id := range ids {
		if _, err := s.attendees.Get(ctx, id, userID); err == nil {
			return true, nil
		} else if !errors.Is(err, repository.ErrNotFound) {
			return false, err
		}
	}
	return false, nil
}

// GetAttendeeOnlySeries returns masterID's series (Master plus Overrides)
// for the CalDAV backend's synthetic Attendee-only collection (ADR-0046,
// #163): it resolves masterID whenever userID is an Attendee of it or of
// any of its Overrides and has no Calendar Access to it — the counterpart
// to GetSeries, which resolves purely through Calendar Access. A series
// userID both has Calendar Access to and is an Attendee of is excluded
// here, mirroring ListAttendeeOnlySeries: it resolves only through
// GetSeries, under its Calendar's own collection, never through this one.
// Returns repository.ErrNotFound if masterID doesn't exist, names an
// Override rather than a Master, userID is not an Attendee of it, or
// userID already has Calendar Access to it.
func (s *EventService) GetAttendeeOnlySeries(ctx context.Context, userID int64, masterID string) (repository.Event, []repository.Event, error) {
	master, err := s.events.GetByID(ctx, masterID)
	if err != nil {
		return repository.Event{}, nil, err
	}
	if master.ParentID != nil {
		return repository.Event{}, nil, repository.ErrNotFound
	}

	if _, err := s.calendarByID(ctx, userID, master.CalendarID); err == nil {
		return repository.Event{}, nil, repository.ErrNotFound
	} else if !errors.Is(err, ErrCalendarNotFound) {
		return repository.Event{}, nil, err
	}

	overridesByParent, err := s.events.ListChildrenByParentIDs(ctx, []string{masterID})
	if err != nil {
		return repository.Event{}, nil, fmt.Errorf("list overrides: %w", err)
	}
	overrides := overridesByParent[masterID]

	isAttendee, err := s.isAttendeeOfEventIDs(ctx, userID, append([]string{masterID}, eventIDs(overrides)...))
	if err != nil {
		return repository.Event{}, nil, err
	}
	if !isAttendee {
		return repository.Event{}, nil, repository.ErrNotFound
	}

	return s.hydrateSeries(ctx, userID, master, overrides)
}

// loadSeriesOverrides loads each of masters' Overrides, returning them both
// keyed by parent id and as one flat row set spanning masters and every
// Override — what a whole-series read hands hydrateEvents, so one batched
// lookup per field covers the Masters and their Overrides together rather
// than one per series. Shared by every read path that recomposes whole
// series: ListSeriesByCalendar, ListAttendeeOnlySeries and SyncSince
// (ADR-0025).
func (s *EventService) loadSeriesOverrides(ctx context.Context, masters []repository.Event) (map[string][]repository.Event, []*repository.Event, error) {
	masterIDs := eventIDs(masters)
	overridesByParent, err := s.events.ListChildrenByParentIDs(ctx, masterIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("list overrides: %w", err)
	}

	// Each Override is pointed at where it already lives in the map, rather
	// than copied into a flat slice and split back out afterwards.
	rows := eventPointers(masters)
	for _, id := range masterIDs {
		overrides := overridesByParent[id]
		for i := range overrides {
			rows = append(rows, &overrides[i])
		}
	}
	return overridesByParent, rows, nil
}

// SyncResult is a sync-collection REPORT's diff: series changed (created or
// updated) since the caller's sync-token, series deleted since then, and the
// new high-water mark to hand back as the next sync-token (ADR-0025).
type SyncResult struct {
	Masters           []repository.Event
	OverridesByParent map[string][]repository.Event
	DeletedUIDs       []string
	NewToken          int64
}

// SyncSince computes calendarID's diff since sinceToken: every Master whose
// series changed (change_seq > sinceToken), every series deleted since then,
// and the calendar's current CTag as the new sync-token. sinceToken of 0
// returns every live series, matching an initial sync (ADR-0025, #65).
func (s *EventService) SyncSince(ctx context.Context, userID int64, calendarID string, sinceToken int64) (SyncResult, error) {
	_, err := s.calendarByID(ctx, userID, calendarID)
	if err != nil {
		if errors.Is(err, ErrCalendarNotFound) {
			return SyncResult{}, nil
		}
		return SyncResult{}, err
	}

	masters, err := s.events.ListMastersChangedSince(ctx, calendarID, sinceToken)
	if err != nil {
		return SyncResult{}, fmt.Errorf("list changed masters: %w", err)
	}

	overridesByParent, rows, err := s.loadSeriesOverrides(ctx, masters)
	if err != nil {
		return SyncResult{}, err
	}
	if err := s.hydrateEvents(ctx, rows, userID, seriesEventFields); err != nil {
		return SyncResult{}, err
	}

	deleted, err := s.sync.DeletedSince(ctx, calendarID, sinceToken)
	if err != nil {
		return SyncResult{}, fmt.Errorf("list deleted objects: %w", err)
	}
	deletedUIDs := make([]string, len(deleted))
	for i, d := range deleted {
		deletedUIDs[i] = d.UID
	}

	newToken, err := s.sync.CTag(ctx, calendarID)
	if err != nil {
		return SyncResult{}, fmt.Errorf("compute new sync-token: %w", err)
	}

	return SyncResult{
		Masters:           masters,
		OverridesByParent: overridesByParent,
		DeletedUIDs:       deletedUIDs,
		NewToken:          newToken,
	}, nil
}

// TouchChangeSeq bumps masterID's change_seq without touching any of its own
// columns, so a write to something outside the events table — an Attachment
// (#132/#133, ADR-0040) — still changes the Master's ETag (SeriesToICal picks
// up the new Attachment on the next read) and its Calendar's CTag. The caller
// has already resolved masterID and checked Access; this trusts that.
func (s *EventService) TouchChangeSeq(ctx context.Context, masterID string) error {
	return s.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return fmt.Errorf("next change seq: %w", err)
		}
		if err := repos.events.SetChangeSeq(ctx, masterID, seq); err != nil {
			return fmt.Errorf("bump change_seq: %w", err)
		}
		return nil
	})
}

// BumpCalendarChangeSeq bumps every live Master under calendarID to a single
// new change_seq (ADR-0064, #213) — the wide counterpart to TouchChangeSeq,
// called after a Calendar default Reminders change: SetDefaultReminders
// itself lives on CalendarService and touches no events row, but resolution
// means every Event on calendarID now reads differently for userID, so CTag
// (and sync-collection's diff) must say so too, or a client holding a
// sync-token never learns anything moved. Same CanRead bar as
// SetDefaultReminders, and the same accepted waste as its own CTag bump:
// shared indiscriminately across every User with Access to calendarID, most
// of whom see no actual content change.
func (s *EventService) BumpCalendarChangeSeq(ctx context.Context, userID int64, calendarID string) error {
	if _, err := s.calendarByID(ctx, userID, calendarID); err != nil {
		return err
	}
	return s.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return fmt.Errorf("next change seq: %w", err)
		}
		if err := repos.events.BumpChangeSeqForCalendar(ctx, calendarID, seq); err != nil {
			return fmt.Errorf("bump calendar change_seq: %w", err)
		}
		return nil
	})
}

// CalendarCTag returns calendarID's CTag — the highest change_seq among its
// live series and its tombstones — for CalDAV's getctag property
// (ADR-0025). Unlike every other EventService method, this never took a
// userID before ADR-0034 — calendarID was an unguessable UUID belonging to
// the only user, so nothing else checked it either. Access now applies here
// too.
func (s *EventService) CalendarCTag(ctx context.Context, userID int64, calendarID string) (int64, error) {
	if _, err := s.calendarByID(ctx, userID, calendarID); err != nil {
		return 0, err
	}
	return s.sync.CTag(ctx, calendarID)
}

// AttendeeOnlyCTag is CalendarCTag's counterpart for the CalDAV backend's
// synthetic Attendee-only collection (#163): no single repository.Calendar
// row (and so no single change_seq/tombstone stream, unlike sync.CTag)
// backs a view spanning every Calendar userID isn't otherwise Accessing, so
// this folds every current member series' own ChangeSeq together with how
// many series currently qualify — an edit to an existing series changes the
// former, an Attendee invite being added, revoked, or newly excluded by
// gaining Calendar Access changes the latter, and either changes the CTag.
func (s *EventService) AttendeeOnlyCTag(ctx context.Context, userID int64) (int64, error) {
	masters, _, err := s.ListAttendeeOnlySeries(ctx, userID)
	if err != nil {
		return 0, err
	}

	ctag := int64(len(masters))
	for _, master := range masters {
		ctag += master.ChangeSeq
	}
	return ctag, nil
}
