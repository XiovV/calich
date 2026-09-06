// connection_writeback.go implements Write-back's send side (#290, #291,
// ADR-0075): draining one queued OutboxKindWriteBack message by rebuilding
// the push from the Event's own live state and PATCHing it to Google, then
// applying the response as though it were a Refresh result (ADR-0076).
//
// Rebuilt from live state, never a stored snapshot, mirroring
// InvitationSender.sendInvitation's own contract (invitation.go) rather than
// its sendCancellation one: a push that sends late still reflects whatever
// the Event looks like right now, including a second edit made before the
// first push ever went out.
//
// #291 adds three things on top of #290's happy path: a bounded conflict
// retry loop against Google's own 412, a Connection whose grant has died
// discovered mid-push, and the permanent-failure marking both of those (and
// the outbox's own ordinary backoff exhaustion, via HandleTerminalFailure)
// funnel into.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/XiovV/calich/server/internal/repository"
)

// maxWriteBackAttempts bounds SendWriteBack's own conflict-retry loop
// (#291, ADR-0075): the first PATCH plus up to two refetch-and-retry
// attempts after a 412, three pushes total, before the Event is marked
// permanently failed. A conflict surviving three of this app's own pushes
// racing the same series isn't a blip the outbox's ordinary backoff would
// ever clear on its own — the two edits are actively fighting over the same
// field, and a human has to look.
const maxWriteBackAttempts = 3

// isGoogleWriteBackConflict reports whether err is Google's 412 for a stale
// If-Match validator on a Write-back PATCH (#291, ADR-0075) — the one status
// SendWriteBack's own retry loop treats as "refetch and retry with the fresh
// etag", as opposed to every other failure, which it lets propagate for the
// outbox's own backoff to handle (or, for a dead access token, handles
// itself — see SendWriteBack).
func isGoogleWriteBackConflict(err error) bool {
	var httpErr *googleHTTPError
	return errors.As(err, &httpErr) && httpErr.statusCode == http.StatusPreconditionFailed
}

// SendWriteBack drains one OutboxKindWriteBack message (#290, #291,
// ADR-0075): it resolves msg.EventID's current row and Calendar, and does
// nothing — a success, not a failure, so the Worker marks msg sent —
// whenever there is genuinely nothing left to push: the Event was deleted
// since msg was queued, it never reached the Provider in the first place
// (ExternalUID nil — Create's own write-back is #292's), its Calendar is no
// longer a writable Linked Calendar (moved elsewhere, unshared, or the
// Provider revoked write access since), or its Connection was disconnected.
// Every one of these is a real outcome a background push can race against,
// not a bug to guard defensively against; none of them should retry forever
// for a state that will never resolve.
//
// A 412 (a stale If-Match) is retried in place, up to maxWriteBackAttempts
// times: refetch the Provider's current copy to learn the fresh etag a 412
// doesn't carry, fold its Provider-owned fields forward locally
// (reconcileProviderOwnedFields), and re-issue the same field-scoped PATCH
// with that etag — our own fields winning outright is simply what a
// field-scoped PATCH already does at Google's end (ADR-0075), so nothing
// here needs to compute a merge. Exhausting the bound, or discovering
// mid-push that the Connection's grant has died, ends the message right
// here via markWriteBackPermanentlyFailed rather than letting the outbox's
// ordinary backoff retry a push that structurally cannot succeed.
func (s *ConnectionService) SendWriteBack(ctx context.Context, msg repository.OutboxMessage) error {
	if !s.configured {
		return ErrGoogleNotConfigured
	}

	event, err := s.events.GetByIDUnchecked(ctx, msg.EventID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("load event for write-back: %w", err)
	}
	if event.ExternalUID == nil {
		return nil
	}

	calendar, err := s.calendars.GetByIDUnchecked(ctx, event.CalendarID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("load calendar for write-back: %w", err)
	}
	if calendar.Source == nil || calendar.Source.Kind != repository.SourceKindConnection || calendar.Source.Mode != repository.SourceModeWritable {
		return nil
	}

	conn, err := s.connections.GetByID(ctx, calendar.UserID, *calendar.Source.ConnectionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("get connection: %w", err)
	}

	exdates, err := s.events.ExdatesFor(ctx, event.ID)
	if err != nil {
		return fmt.Errorf("load exceptions for write-back: %w", err)
	}
	patch := buildGooglePatch(event.Title, event.Start, event.End, event.AllDay, event.Tzid, event.Rrule, exdates, event.Description, event.Location, event.URL)

	accessToken := ""
	if conn.AccessToken != nil {
		accessToken = *conn.AccessToken
	}
	calendarID := *calendar.Source.ExternalCalendarID
	eventID := *event.ExternalUID
	etag := event.ProviderEtag

	var updated googleEventJSON
	for attempt := 1; ; attempt++ {
		updated, err = s.google.patchEvent(ctx, accessToken, calendarID, eventID, etag, patch)
		if isGoogleAccessTokenExpired(err) {
			accessToken, err = s.mintAccessToken(ctx, calendar.UserID, conn)
			if err != nil {
				if class, _ := classifyGoogleError(err); class == ErrorClassNeedsAttention {
					return s.markWriteBackPermanentlyFailed(ctx, calendar.UserID, event.ID, calendar.ID,
						fmt.Sprintf("this connection no longer authenticates with google: %v", err))
				}
				return err
			}
			updated, err = s.google.patchEvent(ctx, accessToken, calendarID, eventID, etag, patch)
		}

		if err == nil {
			break
		}
		if !isGoogleWriteBackConflict(err) {
			return err
		}
		if attempt >= maxWriteBackAttempts {
			return s.markWriteBackPermanentlyFailed(ctx, calendar.UserID, event.ID, calendar.ID,
				"a conflicting edit at google could not be resolved after several attempts")
		}

		fresh, getErr := s.google.getEvent(ctx, accessToken, calendarID, eventID)
		if getErr != nil {
			return getErr
		}
		mapped := toGoogleEvent(fresh)
		etag = googleEtag(mapped.ETag)
		if err := s.reconcileProviderOwnedFields(ctx, event.ID, mapped); err != nil {
			return err
		}
	}

	// Echo suppression (ADR-0075, ADR-0076): see RecordWriteBackEtag's own
	// doc comment.
	return s.events.RecordWriteBackEtag(ctx, event.ID, googleEtag(updated.ETag))
}

// reconcileProviderOwnedFields applies fresh's Provider-owned fields onto
// id's stored row (#291, ADR-0075) — SendWriteBack's own conflict loop is
// the only caller, reached after a 412 forces a refetch of the Provider's
// current copy anyway. Deliberately narrower than a Refresh's own
// reconciler: title, start, end, allDay, tzid, rrule, description, location,
// url and colour are left untouched — those are either this app's own
// pending edit about to be re-pushed with the fresh etag, or (colour)
// governed by the until-touched rule the next ordinary Refresh already
// re-applies within minutes, so this narrower, more time-critical path has
// no need to duplicate it. RSVPStatus, ConferenceURL and GuestCount are the
// Provider's alone, never this app's to have an opinion about, so they
// always move forward unconditionally, exactly as an ordinary Refresh
// already applies them.
func (s *ConnectionService) reconcileProviderOwnedFields(ctx context.Context, id string, fresh googleEvent) error {
	rsvp, guestCount := googleGuestInfo(fresh.Attendees)
	conferenceURL := googleConferenceURL(fresh.ConferenceData)
	return s.events.ApplyProviderOwnedFields(ctx, id, rsvp, conferenceURL, guestCount)
}

// markWriteBackPermanentlyFailed records a Write-back push that will never
// reach the Provider on its own (#291, ADR-0075, ADR-0076): a per-Event
// marker (EventService.MarkWriteBackFailed) so the grid can show it, and the
// affected Linked Calendar's Source raised into needs-attention
// (CalendarService.RecordWriteBackFailure) so the sidebar's existing
// broken-Source badge — already rendered for a failed Refresh — picks this
// up too. Tolerant of the Event or its Calendar having vanished since (a
// concurrent delete, an unshared or moved Calendar): there is nothing left
// to mark, which is success, not failure. Always returns nil on the
// happy/tolerant path so every caller can hand it straight back to the
// outbox Worker as "msg is handled" — retrying a push that would only ever
// fail the same way again serves nobody.
func (s *ConnectionService) markWriteBackPermanentlyFailed(ctx context.Context, userID int64, eventID, calendarID, reason string) error {
	if err := s.events.MarkWriteBackFailed(ctx, eventID, reason); err != nil && !errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("mark event write-back failed: %w", err)
	}
	if err := s.calendars.RecordWriteBackFailure(ctx, userID, calendarID, ErrorClassNeedsAttention, reason); err != nil && !errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("record write-back failure on source: %w", err)
	}
	return nil
}

// MarkPermanentlyFailedFromOutbox is outbox.TerminalFailureHandler's own
// entry point (via OutboxDispatcher) into markWriteBackPermanentlyFailed
// (#291, ADR-0075), for the path SendWriteBack itself never reaches: a
// message that failed the same, non-conflict way (403, an unreachable
// calendar, ...) enough times to exhaust its own backoff schedule
// (outbox.maxAttemptsFor), discovered by the Worker after MarkFailed rather
// than inside one SendWriteBack call. Tolerant of the Event or Calendar
// having vanished since, mirroring SendWriteBack's own no-op cases.
func (s *ConnectionService) MarkPermanentlyFailedFromOutbox(ctx context.Context, msg repository.OutboxMessage, sendErr error) error {
	event, err := s.events.GetByIDUnchecked(ctx, msg.EventID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("load event for terminal write-back failure: %w", err)
	}
	calendar, err := s.calendars.GetByIDUnchecked(ctx, event.CalendarID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("load calendar for terminal write-back failure: %w", err)
	}
	return s.markWriteBackPermanentlyFailed(ctx, calendar.UserID, event.ID, calendar.ID,
		fmt.Sprintf("could not push this event's changes to google: %v", sendErr))
}

// OutboxDispatcher is the outbox.Sender the background Worker drains
// through (#290, ADR-0075): it routes each queued message to the sender for
// its own Kind, so one Worker — its shape entirely unchanged, see
// outbox/worker.go — drains both an Invitation/Cancellation queue and a
// Write-back queue without either kind knowing the other exists. A message
// carrying no recognized Kind falls to Mail — every row this table held
// before #290 has none but the migration's own default, never an empty
// string.
type OutboxDispatcher struct {
	Mail      *InvitationSender
	WriteBack *ConnectionService
}

// Send implements outbox.Sender.
func (d *OutboxDispatcher) Send(ctx context.Context, msg repository.OutboxMessage) error {
	if msg.Kind == repository.OutboxKindWriteBack {
		return d.WriteBack.SendWriteBack(ctx, msg)
	}
	return d.Mail.Send(ctx, msg)
}

// HandleTerminalFailure implements outbox.TerminalFailureHandler (#291,
// ADR-0075): the Worker's own hook for "this message will not be retried
// again", called immediately after it marks msg permanently failed. Only a
// OutboxKindWriteBack message does anything here — mail has no equivalent
// per-Event/per-Source needs-attention marker to raise; ADR-0060 already
// covers what happens to a permanently failed Invitation.
func (d *OutboxDispatcher) HandleTerminalFailure(ctx context.Context, msg repository.OutboxMessage, sendErr error) error {
	if msg.Kind != repository.OutboxKindWriteBack {
		return nil
	}
	return d.WriteBack.MarkPermanentlyFailedFromOutbox(ctx, msg, sendErr)
}
