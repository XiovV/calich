// event_writeback.go is EventService's own narrow seam for
// ConnectionService.SendWriteBack (#290, ADR-0075): rebuilding and recording
// a Write-back push against live state, from the outbox Worker's background
// context where there is no browser session's userID to resolve Access
// against. Every other read/write on an Event belongs through the
// Access-checked methods in event.go instead — these exist only because
// that background context genuinely has none to check.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// GetByIDUnchecked returns id's stored row exactly as EventRepository holds
// it — no Access check, no hydration (no Reminders, no Attendees, no
// Calendar name/colour). SendWriteBack is the only caller: it resolves its
// own Access equivalent by loading the Event's Calendar afterward and
// checking its Source directly, rather than through CalendarService.Access,
// which has no userID to resolve against here.
func (s *EventService) GetByIDUnchecked(ctx context.Context, id string) (repository.Event, error) {
	return s.events.GetByID(ctx, id)
}

// GetOverrideForWriteBack returns masterID's Override for the Occurrence at
// recurrenceID, or repository.ErrNotFound (#293, ADR-0078) — the seam
// SendWriteBack's instance push rebuilds its field-scoped body from, in the
// same background context GetByIDUnchecked already serves. A nil ErrNotFound
// is one of that push's "nothing left to push" no-ops (the Override was
// deleted since the row was queued).
func (s *EventService) GetOverrideForWriteBack(ctx context.Context, masterID string, recurrenceID time.Time) (repository.Event, error) {
	return s.events.GetOverrideByRecurrenceID(ctx, masterID, recurrenceID)
}

// MasterIDsWithPendingWriteBack returns the local ids of every Master
// currently protected by a Pending Write-back push (#290, ADR-0076) — every
// Refresh reconciler's own pending set, subtracted from what it's willing to
// overwrite before it ever compares content, so a Refresh landing inside the
// window between "the local edit committed" and "the push actually reached
// the Provider" can never silently revert that edit back to the Provider's
// still-stale copy. Derived from the outbox rows themselves, not a flag on
// the Event, so queued/retrying/backing-off all count and a crash between
// commit and enqueue is the only hole (ADR-0018 is what closes that one).
func (s *EventService) MasterIDsWithPendingWriteBack(ctx context.Context) (map[string]bool, error) {
	return s.writebackOutbox.ListPendingEventIDsByKind(ctx, repository.OutboxKindWriteBack)
}

// ExternalUIDsWithPendingWriteBackDelete returns the ExternalUID of every
// Master on calendarID with a queued events.delete push (#292, ADR-0077) —
// the delete counterpart to MasterIDsWithPendingWriteBack, and, like it,
// that calendar's Refresh reconciler's own pending set: subtracted from the
// incoming series before absence is ever computed, so a Refresh landing in
// the window between "the local delete committed" and "the delete actually
// reached the Provider" can't re-create the Event the User just removed.
// Keyed by ExternalUID (from the DELETE outbox rows' own snapshots, not a
// query over events, since the local row is already gone) and scoped to
// calendarID, since one Provider event id can name a live series in another
// Linked Calendar.
func (s *EventService) ExternalUIDsWithPendingWriteBackDelete(ctx context.Context, calendarID string) (map[string]bool, error) {
	return s.writebackOutbox.ListPendingWriteBackDeleteExternalUIDs(ctx, calendarID)
}

// AdoptWriteBackIdentity applies a successful events.insert's response as
// though it were a Refresh result (#292, ADR-0077): storing the Provider id
// Google minted and the fresh etag, so the next Refresh reconciles id by
// that id instead of tombstoning it as absent (ADR-0076), and clearing any
// stale permanent-failure marker a previous attempt left. ConnectionService's
// own create push (SendWriteBack) is the only caller.
func (s *EventService) AdoptWriteBackIdentity(ctx context.Context, id, externalUID string, etag *string) error {
	return s.events.AdoptProviderIdentity(ctx, id, externalUID, etag)
}

// RecordWriteBackEtag applies a successful Write-back push's response as
// though it were a Refresh result (#290, ADR-0075, ADR-0076): storing the
// fresh validator Google's PATCH response carried back, so the next Delta
// Refresh's own content comparison finds id already matching what the
// Provider now holds instead of reporting this app's own push back as a
// remote change. Also clears any stale permanent-failure marker a previous
// attempt left behind (#291) — a push landing is exactly the outcome that
// marker exists to flag the absence of.
func (s *EventService) RecordWriteBackEtag(ctx context.Context, id string, etag *string) error {
	if err := s.events.ClearWriteBackError(ctx, id); err != nil {
		return fmt.Errorf("clear write-back error: %w", err)
	}
	return s.events.UpdateProviderEtag(ctx, id, etag)
}

// MarkWriteBackFailed stamps id's row with the per-Event permanent-failure
// marker (#291, ADR-0075, ADR-0076) — ConnectionService's own
// markWriteBackPermanentlyFailed is the only caller, from the same
// background context GetByIDUnchecked already serves.
func (s *EventService) MarkWriteBackFailed(ctx context.Context, id, reason string) error {
	return s.events.MarkWriteBackFailed(ctx, id, reason)
}

// ApplyProviderOwnedFields moves id's Provider-owned RSVPStatus/
// ConferenceURL/GuestCount forward from a fresh Provider fetch (#291,
// ADR-0075) — ConnectionService's own conflict-retry loop
// (reconcileProviderOwnedFields) is the only caller, reached after a 412
// forces a refetch anyway.
func (s *EventService) ApplyProviderOwnedFields(ctx context.Context, id string, rsvpStatus, conferenceURL *string, guestCount int) error {
	return s.events.ApplyProviderOwnedFields(ctx, id, rsvpStatus, conferenceURL, guestCount)
}

// ClearProviderIdentity strips the Provider from every Event of calendarID
// (#295) — the "keep the calendars" disconnect disposition's per-Calendar
// step, turning a Linked Calendar's mirrored Events into ordinary owned
// ones once its Connection is gone. Not Access-checked: its only caller,
// ConnectionService.Disconnect, has already confirmed the Connection — and
// so every Linked Calendar of it — belongs to the caller.
func (s *EventService) ClearProviderIdentity(ctx context.Context, calendarID string) error {
	return s.events.ClearProviderIdentityByCalendar(ctx, calendarID)
}
