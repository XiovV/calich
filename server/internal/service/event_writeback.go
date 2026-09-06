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

// ExdatesFor returns masterID's own Exceptions (ADR-0016) — empty, not an
// error, for a Master with none. SendWriteBack folds these into the
// recurrence array it pushes (encodeGoogleRecurrence), since Google replaces
// that array wholesale and a push that omitted them would silently
// resurrect every occurrence this app already cancelled.
func (s *EventService) ExdatesFor(ctx context.Context, masterID string) ([]time.Time, error) {
	byParent, err := s.exceptions.ListByParentIDs(ctx, []string{masterID})
	if err != nil {
		return nil, fmt.Errorf("list exceptions: %w", err)
	}
	return byParent[masterID], nil
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
