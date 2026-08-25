package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/repository"
)

// ImportSeries writes every series in writes as a brand-new Master (mints
// its own id) plus its Overrides and Exdates, in one transaction taking one
// change_seq (ADR-0030). It is ordinary ICS import's writer, and refuses a
// Subscribed Calendar as its target like every other mutating method
// (ADR-0032) — SubscribeService writes through ImportSubscribedSeries
// instead, since it targets its own Subscribed Calendar deliberately.
// Ordinary import leaves ExternalUID empty, discarding the foreign identity
// entirely; a Subscription's writer sets it so a later Refresh can
// reconcile by it (ADR-0033). Unlike PutSeries, this never updates an
// existing row — an import is always insert-only — and looping PutSeries
// once per series would mean N transactions, N sync bumps, and a
// half-imported Calendar if it died partway through; ImportSeries validates
// every write up front so a bad series in a large import fails before
// anything is written, not partway through the transaction. Returns the
// number of series written.
func (s *EventService) ImportSeries(ctx context.Context, userID int64, calendarID string, writes []SeriesWrite) (int, error) {
	if err := validateSeriesWrites(writes); err != nil {
		return 0, err
	}

	if _, err := s.requireWritableCalendar(ctx, userID, calendarID); err != nil {
		return 0, err
	}

	// An ICS import writes the importing User's Reminders (ADR-0064) — unlike
	// a Subscription's own fetched Events, which belong to the Calendar's
	// Owner (ImportSubscribedSeries below).
	return s.writeSeries(ctx, userID, calendarID, writes, userID)
}

// ImportSubscribedSeries is one of EventService's two bypasses of the
// Subscribed Calendar write guard (ADR-0032), alongside
// ReconcileSubscribedSeries (#85): otherwise identical to ImportSeries, it
// skips requireWritable because writing a Subscription's own fetched Events
// into its own Subscribed Calendar is a legitimate write. Its only caller is
// SubscribeService.Subscribe's initial import — a later Refresh writes
// through ReconcileSubscribedSeries instead, since it must update existing
// rows in place rather than always inserting. Every other write must go
// through ImportSeries or one of the guarded methods instead.
func (s *EventService) ImportSubscribedSeries(ctx context.Context, userID int64, calendarID string, writes []SeriesWrite) (int, error) {
	if err := validateSeriesWrites(writes); err != nil {
		return 0, err
	}

	if _, err := s.calendarByID(ctx, userID, calendarID); err != nil {
		return 0, err
	}

	// A Subscription's own fetched Reminders belong to the Calendar's Owner,
	// not the User who happened to trigger the Subscribe (ADR-0064) — "a
	// Source's alarms belong to the Calendar's Owner".
	ownerID, err := s.reminderOwnerID(ctx, calendarID)
	if err != nil {
		return 0, err
	}
	return s.writeSeries(ctx, userID, calendarID, writes, ownerID)
}

// writeSeries is ImportSeries and ImportSubscribedSeries' shared insert-only
// write, once each has resolved and (except for the bypass) guarded
// calendarID, driving upsertSeries once per write with a freshly minted
// masterID so every write is unconditionally a create. reminderUserID names
// whose Reminders every write's Reminders land under — the importing User
// for a plain ICS import, the Calendar's Owner for a Subscription's own
// fetched Events (ADR-0064) — since the two callers disagree on this
// independently of who userID (the acting/creating User stamped on every
// row) is. Neither import path marks Reminders explicit (see upsertSeries'
// doc comment): nobody told the app "this is my Reminder list" by importing
// a file or subscribing to a feed, so a User's Calendar default must stay
// free to keep applying wherever the file or feed didn't itself specify
// one. Every write shares one already-resolved seq, so importing many
// series in one file bumps change_seq exactly once for the whole batch
// (ADR-0030), not once per series.
func (s *EventService) writeSeries(ctx context.Context, userID int64, calendarID string, writes []SeriesWrite, reminderUserID int64) (int, error) {
	err := s.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}

		for _, w := range writes {
			if err := s.upsertSeries(ctx, repos, calendarID, uuid.NewString(), userID, reminderUserID, seq, w, false); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("import series: %w", err)
	}

	return len(writes), nil
}

// nonEmptyPtr returns nil for "", else a pointer to s — for optional string
// fields (like ExternalUID) that repository.EventFields stores as *string.
func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ReconcileSummary is what a Refresh's DB apply actually did, for the
// caller to report (#85, ADR-0033).
type ReconcileSummary struct {
	Created    int
	Updated    int
	Tombstoned int
}

// ReconcileSubscribedSeries applies result — Refresh's already-decided plan
// (service.ReconcileSeries) — to calendarID inside one transaction: each
// upsert creates a new series (MasterID empty) or updates one in place
// (Overrides matched by RecurrenceID and Exdates replaced wholesale,
// exactly as PutSeries does for a CalDAV PUT), each tombstone deletes an
// existing series outright, cascading to its Overrides. One change_seq bump
// per series actually written; ReconcileSeries has already excluded
// unchanged series from result.Upserts, so those cost nothing here. This is
// ImportSubscribedSeries' sibling bypass of the Subscribed Calendar write
// guard (ADR-0032) — the only other legitimate writer of one, alongside
// Subscribe's initial import.
func (s *EventService) ReconcileSubscribedSeries(ctx context.Context, userID int64, calendarID string, result ReconcileResult) (ReconcileSummary, error) {
	for i, upsert := range result.Upserts {
		title, err := validateEventFields(upsert.Write.Title, upsert.Write.Start, upsert.Write.End, upsert.Write.Reminders)
		if err != nil {
			return ReconcileSummary{}, err
		}
		result.Upserts[i].Write.Title = title
		if !isValidRecurrenceRule(upsert.Write.Rrule) {
			return ReconcileSummary{}, ErrInvalidRecurrenceRule
		}
		for j, o := range upsert.Write.Overrides {
			trimmed, err := validateEventFields(o.Title, o.Start, o.End, o.Reminders)
			if err != nil {
				return ReconcileSummary{}, err
			}
			result.Upserts[i].Write.Overrides[j].Title = trimmed
		}
	}

	calendar, err := s.calendarByID(ctx, userID, calendarID)
	if err != nil {
		return ReconcileSummary{}, err
	}
	// Every Reminder write names the Calendar's Owner, not the reconciling
	// User (ADR-0064 step one, #208) — a Refresh's own KeepAlarms rows
	// belong to the Owner (ADR-0064's "A Source's alarms belong to the
	// Calendar's Owner").
	ownerID := calendar.UserID

	var summary ReconcileSummary
	err = s.withTx(ctx, func(repos txRepos) error {
		for _, upsert := range result.Upserts {
			if upsert.MasterID == "" {
				if err := s.createSubscribedSeries(ctx, repos, userID, ownerID, calendarID, upsert.Write); err != nil {
					return err
				}
				summary.Created++
				continue
			}
			if err := s.updateSubscribedSeries(ctx, repos, userID, ownerID, calendarID, upsert.MasterID, upsert.Write); err != nil {
				return err
			}
			summary.Updated++
		}

		for _, masterID := range result.Tombstones {
			if err := s.tombstoneSubscribedSeries(ctx, repos, calendarID, masterID); err != nil {
				return err
			}
			summary.Tombstoned++
		}

		return nil
	})
	if err != nil {
		return ReconcileSummary{}, fmt.Errorf("reconcile subscribed series: %w", err)
	}

	return summary, nil
}

// createSubscribedSeries mints a new Master (and its Overrides) for write,
// carrying its ExternalUID, via upsertSeries with a freshly minted masterID
// — one change_seq bump for the whole series. ownerID is calendarID's
// Owner — every Reminder write names it, not userID (ADR-0064 step one,
// #208) — and Reminders are never marked explicit (see upsertSeries' doc
// comment): a Subscription's own fetched Reminders are not the Owner
// deliberately asserting a Reminder list, so their Calendar default must
// stay free to keep applying to whatever the feed didn't itself specify.
func (s *EventService) createSubscribedSeries(ctx context.Context, repos txRepos, userID, ownerID int64, calendarID string, write SeriesWrite) error {
	seq, err := repos.sync.NextChangeSeq(ctx)
	if err != nil {
		return err
	}
	return s.upsertSeries(ctx, repos, calendarID, uuid.NewString(), userID, ownerID, seq, write, false)
}

// updateSubscribedSeries updates masterID's row and its Overrides in place
// from write via upsertSeries — the same match-by-RecurrenceID,
// replace-Exdates-wholesale shape PutSeries uses for a CalDAV PUT — one
// change_seq bump for the whole series. masterID's own ExternalUID is never
// touched (it is set on insert only, and repository.EventRepository.Update
// doesn't write that column regardless); Overrides created here inherit it
// from write.ExternalUID, exactly as createSubscribedSeries does. ownerID
// is calendarID's Owner — every Reminder write names it, not userID
// (ADR-0064 step one, #208) — and, like createSubscribedSeries, Reminders
// are never marked explicit.
func (s *EventService) updateSubscribedSeries(ctx context.Context, repos txRepos, userID, ownerID int64, calendarID, masterID string, write SeriesWrite) error {
	seq, err := repos.sync.NextChangeSeq(ctx)
	if err != nil {
		return err
	}
	return s.upsertSeries(ctx, repos, calendarID, masterID, userID, ownerID, seq, write, false)
}

// tombstoneSubscribedSeries deletes masterID outright (cascading to its
// Overrides via the events table's foreign key) and records its removal as
// a tombstone under calendarID, one change_seq bump — mirroring Delete's
// Master case, but bypassing the Subscribed Calendar write guard the way
// every Refresh write does (ADR-0032).
func (s *EventService) tombstoneSubscribedSeries(ctx context.Context, repos txRepos, calendarID, masterID string) error {
	if err := repos.events.Delete(ctx, masterID); err != nil {
		return fmt.Errorf("delete tombstoned series: %w", err)
	}

	seq, err := repos.sync.NextChangeSeq(ctx)
	if err != nil {
		return err
	}

	return repos.sync.Tombstone(ctx, calendarID, masterID, seq)
}

// ClearSubscribedCalendarReminders deletes the Calendar Owner's Reminders on
// calendarID's Events — Masters and Overrides alike — the immediate
// consequence of a Subscription's KeepAlarms being turned off (#87,
// ADR-0032): "off" must mean no Reminders exist, not merely that a future
// Refresh will stop adding them. Scoped to the Owner (ADR-0064 step one,
// #208) since a Source's alarms belong to them alone. Bumps each Master's
// change_seq once so a CalDAV client sees the alarm-less object on its next
// sync, mirroring how an Override's own change is reflected on its Master
// (ADR-0025). This is ImportSubscribedSeries and ReconcileSubscribedSeries'
// third sibling bypass of the Subscribed Calendar write guard (ADR-0032) —
// clearing a Subscription's own Reminders in its own Subscribed Calendar is
// a legitimate write.
func (s *EventService) ClearSubscribedCalendarReminders(ctx context.Context, userID int64, calendarID string) error {
	calendar, err := s.calendarByID(ctx, userID, calendarID)
	if err != nil {
		return err
	}
	ownerID := calendar.UserID

	masters, overridesByParent, err := s.ListSeriesByCalendar(ctx, userID, calendarID)
	if err != nil {
		return err
	}

	return s.withTx(ctx, func(repos txRepos) error {
		for _, m := range masters {
			seq, err := repos.sync.NextChangeSeq(ctx)
			if err != nil {
				return err
			}
			if err := repos.reminders.ReplaceByEventID(ctx, ownerID, m.ID, nil); err != nil {
				return fmt.Errorf("clear master reminders: %w", err)
			}
			for _, o := range overridesByParent[m.ID] {
				if err := repos.reminders.ReplaceByEventID(ctx, ownerID, o.ID, nil); err != nil {
					return fmt.Errorf("clear override reminders: %w", err)
				}
			}
			if err := repos.events.SetChangeSeq(ctx, m.ID, seq); err != nil {
				return fmt.Errorf("bump master change_seq: %w", err)
			}
		}
		return nil
	})
}

// SeriesWrite is a whole series' Master fields plus its Overrides and
// Exdates, decomposed from an incoming CalDAV PUT (ADR-0025).
type SeriesWrite struct {
	Title, Description, Location, URL string
	Start, End                        time.Time
	AllDay                            bool
	Tzid                              *string
	Rrule                             string
	Reminders                         []repository.Reminder
	Exdates                           []time.Time
	Overrides                         []OverrideWrite
	// Color mirrors EventWrite.Color — the Master's own color override, nil
	// meaning "inherit the Calendar's color" (ADR-0043).
	Color *string
	// ExternalUID is set only when ImportSeries is writing a Subscribed
	// Calendar's Events (#83, ADR-0033) — empty for ordinary import and
	// CalDAV PUT, which leave the row's external_uid column NULL.
	ExternalUID string
	// Attachments are Attachments already saved to disk (ADR-0040's
	// before-commit ordering) for writeSeries to row alongside the Master —
	// populated only by ordinary ICS import (#135); every other SeriesWrite
	// source (CalDAV PUT, Subscribe/Refresh) leaves this empty, since
	// Attachments arrive only through Import's own POST actions or ICS
	// import, never through a PUT or an unattended poll.
	Attachments []AttachmentWrite
}

// AttachmentWrite is one Attachment whose bytes are already on disk under
// ID, ready for writeSeries to create its row inside the same transaction as
// its series' Master — ICS import's counterpart to AttachmentService.Upload,
// which does the same two steps (save, then row) outside a series write
// (#135, ADR-0040).
type AttachmentWrite struct {
	ID, Filename, ContentType string
	SizeBytes                 int64
}

// OverrideWrite is one Override VEVENT's fields, keyed by the Occurrence it
// replaces (RecurrenceID).
type OverrideWrite struct {
	RecurrenceID                      time.Time
	Title, Description, Location, URL string
	Start, End                        time.Time
	AllDay                            bool
	Tzid                              *string
	Reminders                         []repository.Reminder
	// ExternalUID mirrors SeriesWrite.ExternalUID — an Override shares its
	// Master's foreign UID (#83, ADR-0033).
	ExternalUID string
	// Color mirrors SeriesWrite.Color, scoped to this Override alone
	// (ADR-0043).
	Color *string
}

// fields projects the Master's own columns onto repository.EventFields for
// upsertSeries' Create or Update call, mirroring EventWrite.fields() —
// Reminders live in their own table, dropped here same as there. calendarID
// is passed in rather than carried on SeriesWrite itself, since every
// SeriesWrite source already resolves and guards it once, up front, the
// same way for a whole series (Master and every Override alike).
func (w SeriesWrite) fields(calendarID string) repository.EventFields {
	return repository.EventFields{
		CalendarID:  calendarID,
		Title:       w.Title,
		Start:       w.Start,
		End:         w.End,
		AllDay:      w.AllDay,
		Rrule:       w.Rrule,
		Tzid:        w.Tzid,
		Description: w.Description,
		Location:    w.Location,
		URL:         w.URL,
		Color:       w.Color,
		ExternalUID: nonEmptyPtr(w.ExternalUID),
	}
}

// fields projects one Override's own columns onto repository.EventFields
// for upsertSeries' Create or Update call, mirroring SeriesWrite.fields().
// masterID and RecurrenceID anchor a freshly-created Override to the
// Occurrence it replaces; harmlessly included but ignored on an Update,
// which never rewrites either (an existing Override's parent and
// recurrence id never change) — see repository.EventRepository.Update's
// column list.
func (o OverrideWrite) fields(calendarID, masterID string) repository.EventFields {
	return repository.EventFields{
		CalendarID:   calendarID,
		Title:        o.Title,
		Start:        o.Start,
		End:          o.End,
		AllDay:       o.AllDay,
		Tzid:         o.Tzid,
		Description:  o.Description,
		Location:     o.Location,
		URL:          o.URL,
		Color:        o.Color,
		ParentID:     &masterID,
		RecurrenceID: &o.RecurrenceID,
		ExternalUID:  nonEmptyPtr(o.ExternalUID),
	}
}

// validateEventFields applies the Create/Update validation shared by every
// VEVENT PutSeries writes (Master or Override), returning the trimmed title.
func validateEventFields(title string, start, end time.Time, reminders []repository.Reminder) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", ErrInvalidTitle
	}
	if !end.After(start) {
		return "", ErrInvalidTimeRange
	}
	if err := validateReminders(reminders); err != nil {
		return "", err
	}
	return title, nil
}

// validateSeriesWrites applies validateSeriesWrite to every write, stopping
// at the first failure without writing anything — the writers' own
// all-or-nothing guard, so a bad series in a large import fails before a
// single row is written rather than partway through the transaction. An
// importer that would rather skip one series than lose the file drops it
// first with dropUnstorableSeries (#228); by the time writes reach here
// they are expected to be storable, and one that isn't is a caller's bug
// rather than a fact about the file.
func validateSeriesWrites(writes []SeriesWrite) error {
	for i := range writes {
		if err := validateSeriesWrite(&writes[i]); err != nil {
			return err
		}
	}
	return nil
}

// validateSeriesWrite is validateSeriesWrites for one series: the whole
// check for a single SeriesWrite and its Overrides, normalizing write in
// place (trimmed title, canonical color) and stopping at the first failure.
// Split out so an importer can ask series by series whether this app can
// store one, and skip just that series rather than failing the whole file
// on it (#228, ADR-0030's "failures are per-series, not per-file").
func validateSeriesWrite(write *SeriesWrite) error {
	title, err := validateEventFields(write.Title, write.Start, write.End, write.Reminders)
	if err != nil {
		return err
	}
	write.Title = title
	if !isValidRecurrenceRule(write.Rrule) {
		return ErrInvalidRecurrenceRule
	}
	color, err := normalizeEventColor(write.Color)
	if err != nil {
		return err
	}
	write.Color = color
	for j, o := range write.Overrides {
		trimmed, err := validateEventFields(o.Title, o.Start, o.End, o.Reminders)
		if err != nil {
			return err
		}
		write.Overrides[j].Title = trimmed
		overrideColor, err := normalizeEventColor(o.Color)
		if err != nil {
			return err
		}
		write.Overrides[j].Color = overrideColor
	}
	return nil
}

// upsertSeries creates masterID's Master row (and its Overrides) if it
// doesn't exist yet, or updates it and every Override in place if it does —
// the single Master+Override write PutSeries, createSubscribedSeries,
// updateSubscribedSeries, and writeSeries all drove independent, drifting
// copies of before #266. Existence is resolved here, from masterID alone:
// each Override in write.Overrides is matched to an existing row by
// RecurrenceID (updated if found, created if not), and any existing
// Override absent from write.Overrides is deleted — the device, feed, or
// import removed it. Exdates are replaced wholesale. write.Attachments (ICS
// import's alone; every other SeriesWrite source leaves it empty) are
// created alongside the Master.
//
// seq is a single already-resolved change_seq, applied to the Master and
// every Override alike, never resolved internally — so a caller writing
// many series in one changeset (writeSeries' bulk ICS import) can pass the
// same seq to every call and bump change_seq exactly once for the whole
// batch, while a caller writing one series on its own (PutSeries, a
// Subscription's create/update reconcile) resolves and passes a fresh seq
// each call instead, one bump per series (ADR-0018/ADR-0025).
//
// userID is the row's created_by; reminderUserID is whose Reminders
// write.Reminders/o.Reminders land under — the two disagree whenever the
// User driving the write isn't whose Reminders they are (ADR-0064): a
// Subscription's own fetched Reminders always belong to the Calendar's
// Owner, never whichever User triggered the fetch or the acting userID.
//
// markExplicit decides whether every Reminder write also marks itself
// explicit (ADR-0064) — deliberately not the same for every caller (#266,
// where this used to differ by accident rather than by design). A CalDAV
// PUT's VALARMs are the PUTting User's own deliberate assertion of their
// Reminder list — indistinguishable from the web app's dedicated
// SetReminders — including an empty VALARM set, so PutSeries passes true:
// a Calendar default must not silently reassert on the User's next
// resolved read. An ICS import and a Subscription's own fetched Reminders
// are the opposite: nobody told the app "this is my Reminder list", so a
// User's Calendar default must stay free to keep applying wherever the
// import or feed didn't itself specify one — writeSeries,
// createSubscribedSeries, and updateSubscribedSeries all pass false.
func (s *EventService) upsertSeries(ctx context.Context, repos txRepos, calendarID, masterID string, userID, reminderUserID, seq int64, write SeriesWrite, markExplicit bool) error {
	// masterID missing is create-vs-update's only signal, deliberately —
	// including for updateSubscribedSeries's caller (ReconcileSubscribedSeries),
	// which believes masterID already exists: recreating a row unexpectedly
	// missing under the same id it was always going to carry self-heals a
	// desynced Refresh plan the same way PutSeries already treats an
	// unknown masterID as a create, rather than the two disagreeing on what
	// "the row I was told about isn't there" means.
	existingMaster, err := repos.events.GetByID(ctx, masterID)
	masterExists := err == nil
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("get existing master: %w", err)
	}

	master := write.fields(calendarID)
	if masterExists {
		// None of upsertSeries' callers carry an iTIP lifecycle of their own
		// (a CalDAV PUT ignores inbound ATTENDEE per ADR-0062; a Subscribed
		// Calendar's Events carry no Attendees at all per ADR-0032) — the
		// row's own iTIP SEQUENCE is simply preserved rather than
		// material-change detected, unlike EventService.Update's own bump.
		if _, err := repos.events.Update(ctx, masterID, master, seq, existingMaster.Sequence); err != nil {
			return fmt.Errorf("update master: %w", err)
		}
	} else {
		if _, err := repos.events.Create(ctx, masterID, &userID, master, seq); err != nil {
			return fmt.Errorf("create master: %w", err)
		}
	}
	if err := repos.reminders.ReplaceByEventID(ctx, reminderUserID, masterID, write.Reminders); err != nil {
		return fmt.Errorf("persist master reminders: %w", err)
	}
	if markExplicit {
		if err := repos.explicitReminders.Mark(ctx, reminderUserID, masterID); err != nil {
			return fmt.Errorf("mark master reminders explicit: %w", err)
		}
	}

	for _, a := range write.Attachments {
		if _, err := repos.attachments.Create(ctx, a.ID, masterID, &userID, a.Filename, a.ContentType, a.SizeBytes); err != nil {
			return fmt.Errorf("create attachment: %w", err)
		}
	}

	if err := repos.exceptions.DeleteByParentID(ctx, masterID); err != nil {
		return fmt.Errorf("clear exdates: %w", err)
	}
	for _, exdate := range write.Exdates {
		if err := repos.exceptions.Add(ctx, masterID, exdate); err != nil {
			return fmt.Errorf("add exdate: %w", err)
		}
	}

	var existingOverrides []repository.Event
	if masterExists {
		overridesByParent, err := repos.events.ListChildrenByParentIDs(ctx, []string{masterID})
		if err != nil {
			return fmt.Errorf("list existing overrides: %w", err)
		}
		existingOverrides = overridesByParent[masterID]
	}
	existingByRecurrenceID := make(map[int64]repository.Event, len(existingOverrides))
	for _, o := range existingOverrides {
		existingByRecurrenceID[o.RecurrenceID.UnixNano()] = o
	}

	seen := make(map[int64]bool, len(write.Overrides))
	for _, o := range write.Overrides {
		key := o.RecurrenceID.UnixNano()
		seen[key] = true

		// An Override never carries a rule of its own (ADR-0016).
		override := o.fields(calendarID, masterID)

		if existing, ok := existingByRecurrenceID[key]; ok {
			if _, err := repos.events.Update(ctx, existing.ID, override, seq, existing.Sequence); err != nil {
				return fmt.Errorf("update override: %w", err)
			}
			if err := repos.reminders.ReplaceByEventID(ctx, reminderUserID, existing.ID, o.Reminders); err != nil {
				return fmt.Errorf("persist override reminders: %w", err)
			}
			if markExplicit {
				if err := repos.explicitReminders.Mark(ctx, reminderUserID, existing.ID); err != nil {
					return fmt.Errorf("mark override reminders explicit: %w", err)
				}
			}
			continue
		}

		overrideID := uuid.NewString()
		if _, err := repos.events.Create(ctx, overrideID, &userID, override, seq); err != nil {
			return fmt.Errorf("create override: %w", err)
		}
		if err := repos.reminders.ReplaceByEventID(ctx, reminderUserID, overrideID, o.Reminders); err != nil {
			return fmt.Errorf("persist override reminders: %w", err)
		}
		if markExplicit {
			if err := repos.explicitReminders.Mark(ctx, reminderUserID, overrideID); err != nil {
				return fmt.Errorf("mark override reminders explicit: %w", err)
			}
		}
	}

	for key, existing := range existingByRecurrenceID {
		if seen[key] {
			continue
		}
		if err := repos.events.Delete(ctx, existing.ID); err != nil {
			return fmt.Errorf("delete removed override: %w", err)
		}
	}

	return nil
}

// PutSeries atomically decomposes a CalDAV PUT into masterID's Master row,
// its Overrides, and its Exdates (ADR-0025) via upsertSeries: masterID is
// created if it doesn't already exist yet, updated in place if it does. The
// whole write bumps change_seq exactly once, so CTag and sync-collection
// see one atomic change (ADR-0018). Returns the written series recomposed
// exactly as GetSeries would.
func (s *EventService) PutSeries(ctx context.Context, userID int64, calendarID, masterID string, write SeriesWrite) (repository.Event, []repository.Event, error) {
	if err := validateSeriesWrite(&write); err != nil {
		return repository.Event{}, nil, err
	}

	if _, err := s.requireWritableCalendar(ctx, userID, calendarID); err != nil {
		return repository.Event{}, nil, err
	}

	existingMaster, err := s.getOwnedEvent(ctx, userID, masterID)
	masterExists := err == nil
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return repository.Event{}, nil, err
	}
	if masterExists && existingMaster.ParentID != nil {
		return repository.Event{}, nil, ErrParentIsOverride
	}
	if masterExists && existingMaster.CalendarID != calendarID {
		return repository.Event{}, nil, repository.ErrNotFound
	}

	err = s.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}
		// A CalDAV PUT carries no iTIP lifecycle of its own (ADR-0062: CalDAV
		// emits ATTENDEE but ignores it inbound), and its VALARMs write the
		// PUTting User's own Reminders, not the Calendar's Owner's
		// (ADR-0064) — hence userID for both userID and reminderUserID, and
		// markExplicit true (see upsertSeries' doc comment).
		return s.upsertSeries(ctx, repos, calendarID, masterID, userID, userID, seq, write, true)
	})
	if err != nil {
		return repository.Event{}, nil, fmt.Errorf("put series: %w", err)
	}

	return s.GetSeries(ctx, userID, masterID)
}
