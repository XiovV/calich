package service

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/XiovV/calich/server/internal/repository"
)

// GetReminders returns userID's own Reminders on eventID, resolved
// (ADR-0064): their own explicit rows if any exist or they've explicitly
// saved an empty list, otherwise their matching Calendar default — empty if
// neither applies. Anyone who can see eventID may call this — Owner,
// Editor, Viewer, or a User-backed Attendee with no Calendar Access at all
// (ADR-0046, ADR-0058) — via getVisibleEvent, exactly the old fan-out's
// recipient set now describing who may set their own Reminder rather than
// who gets nagged (#211).
func (s *EventService) GetReminders(ctx context.Context, userID int64, eventID string) ([]repository.Reminder, error) {
	event, err := s.getVisibleEvent(ctx, userID, eventID)
	if err != nil {
		return nil, err
	}

	resolved, err := s.reminderResolution.Resolve(ctx, []repository.Event{event}, []int64{userID})
	if err != nil {
		return nil, fmt.Errorf("get reminders: %w", err)
	}
	return resolved.For(eventID, userID), nil
}

// SetReminders replaces userID's own Reminders on eventID wholesale
// (ADR-0020, ADR-0064) — a Reminder is personal outright, so this never
// touches another User's rows on the same Event, its title, or its
// SEQUENCE (no Invitation is re-sent, ADR-0059). It does bump eventID's own
// change_seq, so a per-principal CalDAV object (#210) reaches the caller's
// other devices on their next sync — and only theirs, since a CTag bump is
// still shared by the whole Calendar (ADR-0064's accepted CTag waste).
// Not an Event write (#211): it applies to the same recipient set as
// GetReminders — visibility via getVisibleEvent is enough, with no
// requireWritableCalendar call layered on top. It therefore bypasses the
// Viewer restriction and a Source's read-only clamp alike, exactly as
// SetColorOverride does for a Calendar colour (ADR-0038). The reminder
// write, the explicit marker, and the change_seq bump all run inside one
// withTx (ADR-0018, #260) — a failure partway through never leaves the new
// reminders saved without the explicit marker (silently reactivating a
// stale Calendar default) or saved with a stale change_seq (a synced CalDAV
// client never learning of the change).
func (s *EventService) SetReminders(ctx context.Context, userID int64, eventID string, reminders []repository.Reminder) ([]repository.Reminder, error) {
	if err := validateReminders(reminders); err != nil {
		return nil, err
	}

	if _, err := s.getVisibleEvent(ctx, userID, eventID); err != nil {
		return nil, err
	}

	err := s.withTx(ctx, func(repos txRepos) error {
		if err := repos.reminders.ReplaceByEventID(ctx, userID, eventID, reminders); err != nil {
			return fmt.Errorf("set reminders: %w", err)
		}
		// Records that userID's list here is explicit — including this call's
		// own empty reminders, which is what "no Reminders on this one Event"
		// means: their Calendar default stops applying to it (ADR-0064).
		if err := repos.explicitReminders.Mark(ctx, userID, eventID); err != nil {
			return fmt.Errorf("mark reminders explicit: %w", err)
		}
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return fmt.Errorf("next change seq: %w", err)
		}
		if err := repos.events.SetChangeSeq(ctx, eventID, seq); err != nil {
			return fmt.Errorf("bump change_seq: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return reminders, nil
}

// attachCalendarMeta stamps each Event's CalendarName/CalendarColor
// (display-only, mirroring attachCreatedByNames) from known — the caller's
// own Calendar-Access set List already loaded — falling back to a single
// batched, access-unchecked Calendar lookup for whichever Events' Calendars
// aren't in known: an Attendee-only Event (ADR-0046), whose caller has no
// Access row to have populated known with in the first place. That fallback
// performs no visibility check of its own; it's safe only because every
// caller of List/Get has already established visibility into the Event
// itself.
func (s *EventService) attachCalendarMeta(ctx context.Context, rows []*repository.Event, known map[string]CalendarWithAccess) error {
	resolved := make(map[string]CalendarMeta, len(known))
	for id, c := range known {
		resolved[id] = CalendarMeta{Name: c.Name, Color: c.Color}
	}

	var missing []string
	seen := make(map[string]bool)
	for _, e := range rows {
		if _, ok := resolved[e.CalendarID]; ok || seen[e.CalendarID] {
			continue
		}
		seen[e.CalendarID] = true
		missing = append(missing, e.CalendarID)
	}
	if len(missing) > 0 {
		fetched, err := s.calendars.AttendeeCalendarMetaByIDs(ctx, missing)
		if err != nil {
			return fmt.Errorf("resolve calendar meta: %w", err)
		}
		for id, meta := range fetched {
			resolved[id] = meta
		}
	}

	for _, e := range rows {
		meta := resolved[e.CalendarID]
		e.CalendarName = meta.Name
		e.CalendarColor = meta.Color
	}
	return nil
}

// ListAllWithReminders returns every Event that could fire a Reminder in the
// window (from, to] — one tick of the firing engine (ADR-0021) — alongside the
// resolution naming every User who fires one on it (ADR-0064). The same
// resolution the web app, CalDAV and ICS export read through, answered for
// everyUser instead of one viewer, so the two cannot drift (#216).
//
// The window bounds the read, not the answer: it is widened generously below
// and the engine prunes to the exact trigger instant itself, so what comes back
// is a superset of what fires — including the Overrides that fire nothing but
// shadow an Occurrence out of their Master's expansion (withShadowingOverrides).
func (s *EventService) ListAllWithReminders(ctx context.Context, from, to time.Time) ([]repository.Event, repository.ResolvedReminders, error) {
	// candidates accumulates every Event that might fire a Reminder: those
	// carrying a Reminder row of somebody's, plus every Event on a Calendar
	// carrying at least one User's default, since any of those could resolve
	// one even with zero event_reminders rows of its own.
	candidates, err := s.events.ListAllWithAnyReminder(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list events with reminders: %w", err)
	}

	calendarIDs, err := s.calendarDefaults.CalendarIDsWithAny(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list calendars with default reminders: %w", err)
	}
	if len(calendarIDs) > 0 {
		windowStart, windowEnd, err := s.defaultReminderCandidateWindow(ctx, from, to)
		if err != nil {
			return nil, nil, err
		}
		onCalendarsWithDefaults, err := s.events.ListByCalendarIDs(ctx, calendarIDs, &windowStart, &windowEnd)
		if err != nil {
			return nil, nil, fmt.Errorf("list events for default reminders: %w", err)
		}
		candidates = mergeEventsByID(candidates, onCalendarsWithDefaults)
	}

	resolved, err := s.reminderResolution.Resolve(ctx, candidates, everyUser)
	if err != nil {
		return nil, nil, err
	}

	events := make([]repository.Event, 0, len(candidates))
	for _, e := range candidates {
		if len(resolved[e.ID]) > 0 {
			events = append(events, e)
		}
	}

	events, err = s.withShadowingOverrides(ctx, events)
	if err != nil {
		return nil, nil, err
	}

	// Exdates alone: the engine reads nothing else off these rows, and unlike
	// every other read path they reach nobody, so they carry no viewer's
	// fields and name no hydration recipe (#215).
	if err := s.attachExdates(ctx, eventPointers(events)); err != nil {
		return nil, nil, err
	}
	return events, resolved, nil
}

// withShadowingOverrides adds every Override of the recurring Masters in
// events, whether or not it fires anything itself — the one part of the firing
// read no window may bound.
//
// A Master's own RRULE keeps generating the Occurrence slot an Override has
// replaced, since creating an Override adds no Exdate, and the engine
// suppresses that slot only by finding the Override among the Events it was
// handed (DueAll). An Occurrence may be moved arbitrarily far from the slot it
// replaces, so no widening could reliably catch its Override, and an Override
// resolving a Calendar default carries no Reminder row to pull it in either —
// without this, a windowed read leaves the Master firing a stale cue for a slot
// that no longer exists (#219).
//
// Only shadowing is at stake: an Override that could itself fire in this window
// starts inside it, so it is already a candidate and already carries its own
// resolution, which the merge leaves untouched.
func (s *EventService) withShadowingOverrides(ctx context.Context, events []repository.Event) ([]repository.Event, error) {
	masterIDs := make([]string, 0, len(events))
	for _, e := range events {
		if e.ParentID == nil && e.Rrule != "" {
			masterIDs = append(masterIDs, e.ID)
		}
	}
	if len(masterIDs) == 0 {
		return events, nil
	}

	overridesByParent, err := s.events.ListChildrenByParentIDs(ctx, masterIDs)
	if err != nil {
		return nil, fmt.Errorf("list overrides for firing: %w", err)
	}

	overrides := make([]repository.Event, 0, len(overridesByParent))
	for _, byParent := range overridesByParent {
		overrides = append(overrides, byParent...)
	}
	return mergeEventsByID(events, overrides), nil
}

// firingCandidateSlack is generous slack added to either end of the
// Calendar-default candidate window, on top of the widening by the offsets
// themselves: an all-day Occurrence's anchor sits 9 hours after its row's
// stored midnight start (ADR-0020), and the window's own comparisons are
// strict at both ends. The firing engine recomputes every anchor and prunes to
// the exact trigger, so this only has to be wide enough never to lose a
// candidate.
const firingCandidateSlack = 24 * time.Hour

// defaultReminderCandidateWindow bounds the Events worth reading for
// Calendar-default resolution on a tick covering (from, to] — the read that
// was open-ended before #219, and so grew with a Calendar's whole history
// rather than with what fires on it.
//
// A Reminder's trigger is its Occurrence's anchor minus its own offset, so an
// Occurrence firing in this tick has its anchor in [from+minOffset,
// to+maxOffset] — the window has to be widened by the offsets in play rather
// than assumed small, since an offset may be arbitrarily long, and by the
// smallest as well as the largest, since an offset decoded from an inbound
// CalDAV TRIGGER may be negative (fire *after* the Occurrence).
//
// The Defaults' offsets are the only ones that matter here: an Event carrying
// a Reminder row of anybody's is already a candidate via ListAllWithAnyReminder,
// which this window never narrows, so a default-resolved fire is the only kind
// this read is responsible for finding.
func (s *EventService) defaultReminderCandidateWindow(ctx context.Context, from, to time.Time) (windowStart, windowEnd time.Time, err error) {
	minOffset, maxOffset, err := s.calendarDefaults.OffsetMinutesRange(ctx)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("read default reminder offsets: %w", err)
	}
	windowStart = from.Add(time.Duration(minOffset)*time.Minute - firingCandidateSlack)
	windowEnd = to.Add(time.Duration(maxOffset)*time.Minute + firingCandidateSlack)
	return windowStart, windowEnd, nil
}

// rowIDs is eventIDs over hydrateEvents' row form.
func rowIDs(rows []*repository.Event) []string {
	ids := make([]string, len(rows))
	for i, e := range rows {
		ids[i] = e.ID
	}
	return ids
}

// eventFields is a hydration recipe: which of an Event's derived fields
// hydrateEvents fills in. Every read path that hands an Event to somebody names
// one of the two recipes below rather than assembling its own sequence, so
// "which fields does an Event carry here" has one answer per recipe instead of
// one per call site (#215) — restEventFields the Event API, seriesEventFields
// the iCalendar codec, and between them that is every Event anyone ever sees.
//
// ListAllWithReminders is the one read that names neither, and it is the
// exception the rule is drawn around: its rows reach nobody, so it fills in the
// single field the firing engine reads off them (Exdates) directly rather than
// naming a recipe that would have to say "and none of the rest" (#216).
type eventFields struct {
	calendarMeta           bool
	exdates                bool
	viewerReminders        bool
	createdByNames         bool
	attachments            bool
	attendeeCounts         bool
	organizersAndAttendees bool

	// knownCalendars answers calendarMeta for the Calendars the caller has
	// already loaded, so List doesn't re-query its own Access set. Nil is
	// fine — every Calendar is then looked up.
	knownCalendars map[string]CalendarWithAccess
}

// withKnownCalendars returns f with known filled in, leaving f itself
// untouched so the package-level recipes stay immutable.
func (f eventFields) withKnownCalendars(known map[string]CalendarWithAccess) eventFields {
	f.knownCalendars = known
	return f
}

// restEventFields is what an Event carries in an Event API response —
// List, Get, Create and Update alike (#215). The web app renders the
// Calendar's name and colour, the creator's name and the Attendee count
// beside the Event, so every path that hands it one fills them in.
var restEventFields = eventFields{
	calendarMeta:    true,
	exdates:         true,
	viewerReminders: true,
	createdByNames:  true,
	attachments:     true,
	attendeeCounts:  true,
}

// seriesEventFields is what a whole series carries on the way to the
// iCalendar codec — CalDAV's object reads and ICS export (#215). It trades
// the display-only fields for the Organizer/Attendee mailto addresses
// SeriesToICal needs (ADR-0062), and keeps Attachments, which ATTACH is
// rendered from (ADR-0040).
var seriesEventFields = eventFields{
	exdates:                true,
	viewerReminders:        true,
	attachments:            true,
	organizersAndAttendees: true,
}

// hydrateEvents fills in the fields the recipe names on rows, as viewerID
// sees them — the one place an Event acquires any derived field, so a path that
// wants one names it rather than remembering to call for it. Each field costs
// one batched query across every row regardless of how many there are; rows
// need not be contiguous in memory, so a Master and its Overrides hydrate
// together in one pass.
//
// The attach calls below run concurrently via errgroup (#273): each writes
// only fields of its own, except createdByNames and organizersAndAttendees,
// which both write CreatedByName — restEventFields and seriesEventFields
// never enable both on the same call, so that's not a live race, but a
// recipe that did would need to pick one. attachResolvedViewerReminders
// needs its own snapshot of every row for the same reason: it takes one
// synchronously, before any goroutine below can mutate rows, so its read
// never races with their writes.
func (s *EventService) hydrateEvents(ctx context.Context, rows []*repository.Event, viewerID int64, fields eventFields) error {
	if len(rows) == 0 {
		return nil
	}

	var reminderSnapshot []repository.Event
	if fields.viewerReminders {
		reminderSnapshot = make([]repository.Event, len(rows))
		for i, e := range rows {
			reminderSnapshot[i] = *e
		}
	}

	g, ctx := errgroup.WithContext(ctx)

	if fields.calendarMeta {
		g.Go(func() error { return s.attachCalendarMeta(ctx, rows, fields.knownCalendars) })
	}
	if fields.exdates {
		g.Go(func() error { return s.attachExdates(ctx, rows) })
	}
	if fields.viewerReminders {
		g.Go(func() error { return s.attachResolvedViewerReminders(ctx, rows, reminderSnapshot, viewerID) })
	}
	if fields.createdByNames {
		g.Go(func() error { return s.attachCreatedByNames(ctx, rows) })
	}
	if fields.attachments {
		g.Go(func() error { return s.attachAttachments(ctx, rows) })
	}
	if fields.attendeeCounts {
		g.Go(func() error { return s.attachAttendeeCounts(ctx, rows) })
	}
	if fields.organizersAndAttendees {
		g.Go(func() error { return s.attachOrganizersAndAttendees(ctx, rows) })
	}

	return g.Wait()
}

// attachExdates fills in Exdates from each row's Exceptions (ADR-0016). An
// Override never has any of its own — Exceptions hang off the Master.
func (s *EventService) attachExdates(ctx context.Context, rows []*repository.Event) error {
	exceptionsByParent, err := s.exceptions.ListByParentIDs(ctx, rowIDs(rows))
	if err != nil {
		return fmt.Errorf("list exceptions: %w", err)
	}

	for _, e := range rows {
		e.Exdates = exceptionsByParent[e.ID]
	}
	return nil
}

// attachCreatedByNames fills in CreatedByName on rows whose
// CreatedBy is set (#118), batching one query across every distinct creator
// rather than one per Event — the same shape as attachResolvedViewerReminders
// and attachExdates. An Event whose creator has since been deleted, or with no
// recorded creator at all, is simply left with an empty
// CreatedByName.
func (s *EventService) attachCreatedByNames(ctx context.Context, rows []*repository.Event) error {
	ids := make([]int64, 0, len(rows))
	seen := make(map[int64]bool)
	for _, e := range rows {
		if e.CreatedBy == nil || seen[*e.CreatedBy] {
			continue
		}
		seen[*e.CreatedBy] = true
		ids = append(ids, *e.CreatedBy)
	}

	users, err := s.users.GetByIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("list creators: %w", err)
	}

	for _, e := range rows {
		if e.CreatedBy == nil {
			continue
		}
		e.CreatedByName = users[*e.CreatedBy].Name
	}
	return nil
}

// attachAttendeeCounts fills in AttendeeCount on every Event, batching one
// query across all of them rather than one per Event — the same shape as
// attachCreatedByNames. Unlike attachAttachments, this is not restricted to
// Masters: an Override can carry its own Attendees, and #193's
// progressive-disclosure rule needs an accurate count on whichever Event it
// was handed.
func (s *EventService) attachAttendeeCounts(ctx context.Context, rows []*repository.Event) error {
	userIDsByEvent, err := s.attendees.ListUserIDsByEventIDs(ctx, rowIDs(rows))
	if err != nil {
		return fmt.Errorf("list attendee counts: %w", err)
	}

	for _, e := range rows {
		e.AttendeeCount = len(userIDsByEvent[e.ID])
	}
	return nil
}

// attachResolvedViewerReminders fills in Reminders on rows with viewerID's
// resolved Reminders — one call into the resolution module (ADR-0064, #216),
// projected to the one User asking. Both recipes' Reminder step, so a Reminder
// that exists only by Calendar-default resolution appears as a VALARM exactly
// like an explicit one (#213): CalDAV/ICS export do not stay scoped to explicit
// rows alone. values is a snapshot of rows taken by the caller (hydrateEvents)
// before hydration starts, rather than copied here, since hydrateEvents runs
// this alongside other attach calls that mutate rows (#273) and a copy taken
// mid-flight would race with their writes.
func (s *EventService) attachResolvedViewerReminders(ctx context.Context, rows []*repository.Event, values []repository.Event, viewerID int64) error {
	resolved, err := s.reminderResolution.Resolve(ctx, values, []int64{viewerID})
	if err != nil {
		return err
	}
	for _, e := range rows {
		e.Reminders = resolved.For(e.ID, viewerID)
	}
	return nil
}

// attachAttachments fills in Attachments on each Master in rows (an
// Override is left with none — it can never carry its own, ADR-0040),
// plus UploadedByName on each of them, batching one Attachment lookup
// and one user lookup across every Event rather than one per Event, the
// same shape as attachResolvedViewerReminders and attachCreatedByNames.
func (s *EventService) attachAttachments(ctx context.Context, rows []*repository.Event) error {
	masterIDs := make([]string, 0, len(rows))
	for _, e := range rows {
		if e.ParentID == nil {
			masterIDs = append(masterIDs, e.ID)
		}
	}

	attachmentsByEvent, err := s.attachments.ListByEventIDs(ctx, masterIDs)
	if err != nil {
		return fmt.Errorf("list attachments: %w", err)
	}

	uploaderIDs := make([]int64, 0)
	seen := make(map[int64]bool)
	for _, list := range attachmentsByEvent {
		for _, a := range list {
			if a.UploadedBy == nil || seen[*a.UploadedBy] {
				continue
			}
			seen[*a.UploadedBy] = true
			uploaderIDs = append(uploaderIDs, *a.UploadedBy)
		}
	}
	uploaders, err := s.users.GetByIDs(ctx, uploaderIDs)
	if err != nil {
		return fmt.Errorf("list uploaders: %w", err)
	}

	for _, e := range rows {
		if e.ParentID != nil {
			continue
		}
		list := attachmentsByEvent[e.ID]
		for j := range list {
			if list[j].UploadedBy != nil {
				list[j].UploadedByName = uploaders[*list[j].UploadedBy].Name
			}
		}
		e.Attachments = list
	}
	return nil
}

// attachOrganizersAndAttendees fills in each row's Attendees (with
// Name/Email) and its Organizer's CreatedByName/CreatedByEmail, batching one
// query across all of them rather than one per Event — the CalDAV/ICS
// codec's read path (ADR-0062), which needs the mailto addresses
// attachCreatedByNames and attachAttendeeCounts don't carry. Scoped per
// Event row: a Master and each of its Overrides can each carry their own
// Attendees and their own Organizer (#193, ADR-0055).
func (s *EventService) attachOrganizersAndAttendees(ctx context.Context, rows []*repository.Event) error {
	attendeesByEvent, err := s.attendees.ListWithNamesByEventIDs(ctx, rowIDs(rows))
	if err != nil {
		return fmt.Errorf("list attendees: %w", err)
	}

	organizerIDs := make([]int64, 0)
	seen := make(map[int64]bool)
	for _, e := range rows {
		if e.CreatedBy == nil || seen[*e.CreatedBy] {
			continue
		}
		seen[*e.CreatedBy] = true
		organizerIDs = append(organizerIDs, *e.CreatedBy)
	}
	organizers, err := s.users.GetByIDs(ctx, organizerIDs)
	if err != nil {
		return fmt.Errorf("list organizers: %w", err)
	}

	for _, e := range rows {
		e.Attendees = attendeesByEvent[e.ID]
		if e.CreatedBy != nil {
			e.CreatedByName = organizers[*e.CreatedBy].Name
			e.CreatedByEmail = organizers[*e.CreatedBy].Email
		}
	}
	return nil
}
