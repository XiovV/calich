// connection_writeback.go implements Write-back's send side (#290, #291,
// #292, ADR-0075, ADR-0077): draining one queued OutboxKindWriteBack message
// by its Method — PATCH an existing Provider event, POST (events.insert) a
// locally created one and adopt the id Google returns, or DELETE
// (events.delete) one removed here — then applying the response as though it
// were a Refresh result (ADR-0076).
//
// PATCH and POST are rebuilt from the Event's own live state, never a stored
// snapshot, mirroring InvitationSender.sendInvitation's own contract
// (invitation.go): a push that sends late still reflects whatever the Event
// looks like right now, including a second edit made before the first push
// ever went out. A DELETE cannot — the local row is already gone — so it
// carries a snapshot, like sendCancellation.
//
// #291 added a bounded conflict retry loop against Google's own 412, a
// Connection whose grant has died discovered mid-push, and the
// permanent-failure marking both of those (and the outbox's own ordinary
// backoff exhaustion, via HandleTerminalFailure) funnel into.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"github.com/XiovV/calich/server/internal/repository"
)

// googleClientEventID derives a stable, Google-valid event id from a local
// Event id (#292, ADR-0077). Google's id grammar is base32hex (0-9, a-v),
// 5–1024 chars; a plain hash of the local id is always in range whatever the
// local id's own shape (a UUID in production, a test literal otherwise), and
// being deterministic is what makes events.insert idempotent on retry — the
// second POST carries the same id and Google answers 409.
func googleClientEventID(localID string) string {
	sum := sha256.Sum256([]byte(localID))
	return hex.EncodeToString(sum[:])[:32]
}

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

// SendWriteBack drains one OutboxKindWriteBack message (#290, #291, #292,
// ADR-0075, ADR-0077) by its Method:
//
//   - PATCH — an edit to a Master that already exists at the Provider.
//   - POST — events.insert for a locally created Master with no ExternalUID
//     yet; the id Google returns is adopted onto the local row so the next
//     Refresh reconciles by it rather than tombstoning it as absent
//     (ADR-0076).
//   - DELETE — events.delete for a Master removed here, addressed by the
//     snapshot the row carries since the local row is already gone.
//
// It does nothing — a success, not a failure, so the Worker marks msg sent —
// whenever there is genuinely nothing left to push: the Event was deleted
// since msg was queued (PATCH/POST), a POST's Event turns out to already
// carry an ExternalUID, a PATCH's Event never reached the Provider, the
// Calendar is no longer a writable Linked Calendar (moved, unshared, write
// access revoked), or the Connection was disconnected. None of these should
// retry forever for a state that will never resolve.
//
// A 412 (a stale If-Match) is retried in place, up to maxWriteBackAttempts
// times, refetching the Provider's fresh etag between attempts. Exhausting
// the bound, or discovering mid-push that the Connection's grant has died,
// ends the message here via markWriteBackPermanentlyFailed rather than
// letting the outbox retry a push that structurally cannot succeed.
func (s *ConnectionService) SendWriteBack(ctx context.Context, msg repository.OutboxMessage) error {
	if !s.configured {
		return ErrGoogleNotConfigured
	}
	if msg.Method == repository.OutboxMethodDelete {
		return s.sendWriteBackDelete(ctx, msg)
	}
	return s.sendWriteBackUpsert(ctx, msg)
}

// writeBackContext is the Calendar and Connection a push runs against, once
// resolveWritableLinkedContext has confirmed the Calendar is still a
// writable Linked Calendar whose Connection still exists.
type writeBackContext struct {
	calendar   repository.Calendar
	conn       repository.Connection
	calendarID string // the Provider's own calendar id
}

// resolveWritableLinkedContext loads calendarID's Calendar and its
// Connection, returning ok=false (and a nil error) for every "nothing left
// to push" race SendWriteBack's own doc comment names: the Calendar gone,
// no longer a writable Connection Source, or its Connection disconnected.
func (s *ConnectionService) resolveWritableLinkedContext(ctx context.Context, calendarID string) (writeBackContext, bool, error) {
	calendar, err := s.calendars.GetByIDUnchecked(ctx, calendarID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return writeBackContext{}, false, nil
		}
		return writeBackContext{}, false, fmt.Errorf("load calendar for write-back: %w", err)
	}
	if calendar.Source == nil || calendar.Source.Kind != repository.SourceKindConnection || calendar.Source.Mode != repository.SourceModeWritable {
		return writeBackContext{}, false, nil
	}
	conn, err := s.connections.GetByID(ctx, calendar.UserID, *calendar.Source.ConnectionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return writeBackContext{}, false, nil
		}
		return writeBackContext{}, false, fmt.Errorf("get connection: %w", err)
	}
	return writeBackContext{calendar: calendar, conn: conn, calendarID: *calendar.Source.ExternalCalendarID}, true, nil
}

// accessToken returns the Connection's cached access token, "" when it has
// none — the caller mints a fresh one only once that has actually failed.
func (wc writeBackContext) accessToken() string {
	if wc.conn.AccessToken != nil {
		return *wc.conn.AccessToken
	}
	return ""
}

// failOnDeadGrant maps a mintAccessToken failure to either a permanent
// write-back failure (the grant is gone — ADR-0075's Connection-death case,
// shared by all three methods) or the raw error for the outbox to back off
// on. eventID may name a row already deleted (a DELETE push):
// markWriteBackPermanentlyFailed tolerates that and still raises the Source.
func (s *ConnectionService) failOnDeadGrant(ctx context.Context, userID int64, eventID, calendarID string, err error) error {
	if class, _ := classifyGoogleError(err); class == ErrorClassNeedsAttention {
		return s.markWriteBackPermanentlyFailed(ctx, userID, eventID, calendarID,
			fmt.Sprintf("this connection no longer authenticates with google: %v", err))
	}
	return err
}

// sendWriteBackUpsert drains a PATCH or POST message: it rebuilds the
// field-scoped body from the Event's own live state, then either inserts it
// (POST, no ExternalUID) and adopts the id Google returns, or patches the
// existing Provider event with the bounded conflict-retry loop #291 built.
func (s *ConnectionService) sendWriteBackUpsert(ctx context.Context, msg repository.OutboxMessage) error {
	event, err := s.events.GetByIDUnchecked(ctx, msg.EventID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("load event for write-back: %w", err)
	}

	isCreate := event.ExternalUID == nil
	if isCreate && msg.Method != repository.OutboxMethodPost {
		// A PATCH for an Event that never reached the Provider — #290's own
		// "never reached the Provider" no-op.
		return nil
	}

	wc, ok, err := s.resolveWritableLinkedContext(ctx, event.CalendarID)
	if err != nil || !ok {
		return err
	}

	exdates, err := s.events.ExdatesFor(ctx, event.ID)
	if err != nil {
		return fmt.Errorf("load exceptions for write-back: %w", err)
	}
	patch := buildGooglePatch(event.Title, event.Start, event.End, event.AllDay, event.Tzid, event.Rrule, exdates, event.Description, event.Location, event.URL)
	accessToken := wc.accessToken()

	if isCreate {
		// A client-supplied id makes the insert idempotent: a retry after
		// Google committed the create but lost the response answers 409,
		// which insertEvent resolves to the existing event rather than a
		// duplicate (#292, ADR-0077).
		patch.ID = googleClientEventID(event.ID)

		inserted, err := s.google.insertEvent(ctx, accessToken, wc.calendarID, patch)
		if isGoogleAccessTokenExpired(err) {
			accessToken, err = s.mintAccessToken(ctx, wc.calendar.UserID, wc.conn)
			if err != nil {
				return s.failOnDeadGrant(ctx, wc.calendar.UserID, event.ID, wc.calendar.ID, err)
			}
			inserted, err = s.google.insertEvent(ctx, accessToken, wc.calendarID, patch)
		}
		if err != nil {
			return err
		}
		// Applied as though it were a Refresh result (ADR-0076): the Provider
		// id and etag land on the row so the next Refresh reconciles by it.
		if err := s.events.AdoptWriteBackIdentity(ctx, event.ID, inserted.ID, googleEtag(inserted.ETag)); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				// The Event was deleted locally while this insert was in
				// flight (ADR-0077's create/delete race). EventService.Delete
				// has already queued an events.delete against this exact id
				// (googleClientEventID is deterministic), so the orphan Google
				// just created is cleaned up by that push — nothing to do here.
				return nil
			}
			return err
		}
		return nil
	}

	eventID := *event.ExternalUID
	etag := event.ProviderEtag

	var updated googleEventJSON
	for attempt := 1; ; attempt++ {
		updated, err = s.google.patchEvent(ctx, accessToken, wc.calendarID, eventID, etag, patch)
		if isGoogleAccessTokenExpired(err) {
			accessToken, err = s.mintAccessToken(ctx, wc.calendar.UserID, wc.conn)
			if err != nil {
				return s.failOnDeadGrant(ctx, wc.calendar.UserID, event.ID, wc.calendar.ID, err)
			}
			updated, err = s.google.patchEvent(ctx, accessToken, wc.calendarID, eventID, etag, patch)
		}

		if err == nil {
			break
		}
		if !isGoogleWriteBackConflict(err) {
			return err
		}
		if attempt >= maxWriteBackAttempts {
			return s.markWriteBackPermanentlyFailed(ctx, wc.calendar.UserID, event.ID, wc.calendar.ID,
				"a conflicting edit at google could not be resolved after several attempts")
		}

		fresh, getErr := s.google.getEvent(ctx, accessToken, wc.calendarID, eventID)
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

// sendWriteBackDelete drains a DELETE message (#292, ADR-0077): the local
// row is gone, so everything the push needs comes from the row's snapshot.
// The delete is unconditional (deleteEvent's own doc comment) — a 404/410 at
// Google is success, the end state the User asked for. Any other failure
// propagates for the outbox to back off on; exhausting that raises the
// Linked Calendar's Source into needs-attention (MarkPermanentlyFailedFromOutbox),
// the only surface left once there is no Event row to mark.
//
// If the Calendar is no longer writable, or its Connection is gone, the push
// cannot be delivered — but unlike a PATCH/POST no-op, staying silent here
// means the User's delete is quietly undone by the next Refresh, which still
// lists the event. So those cases raise the Source's needs-attention marker
// rather than returning a bare nil. Only the Calendar having vanished
// entirely is a true nothing-to-do.
func (s *ConnectionService) sendWriteBackDelete(ctx context.Context, msg repository.OutboxMessage) error {
	snap := msg.WriteBackDelete
	if snap == nil || snap.ExternalUID == "" {
		// The Event never reached the Provider — nothing to delete there.
		return nil
	}

	calendar, err := s.calendars.GetByIDUnchecked(ctx, snap.CalendarID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("load calendar for write-back delete: %w", err)
	}
	if calendar.Source == nil || calendar.Source.Kind != repository.SourceKindConnection {
		return nil
	}
	if calendar.Source.Mode != repository.SourceModeWritable {
		return s.markWriteBackPermanentlyFailed(ctx, calendar.UserID, msg.EventID, calendar.ID,
			"this linked calendar is no longer writable, so deleting this event could not be pushed to google")
	}
	conn, err := s.connections.GetByID(ctx, calendar.UserID, *calendar.Source.ConnectionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return s.markWriteBackPermanentlyFailed(ctx, calendar.UserID, msg.EventID, calendar.ID,
				"this linked calendar's connection was removed, so deleting this event could not be pushed to google")
		}
		return fmt.Errorf("get connection: %w", err)
	}

	accessToken := ""
	if conn.AccessToken != nil {
		accessToken = *conn.AccessToken
	}
	calendarID := *calendar.Source.ExternalCalendarID

	err = s.google.deleteEvent(ctx, accessToken, calendarID, snap.ExternalUID)
	if isGoogleAccessTokenExpired(err) {
		accessToken, err = s.mintAccessToken(ctx, calendar.UserID, conn)
		if err != nil {
			return s.failOnDeadGrant(ctx, calendar.UserID, msg.EventID, snap.CalendarID, err)
		}
		err = s.google.deleteEvent(ctx, accessToken, calendarID, snap.ExternalUID)
	}
	return err
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
	// A DELETE row's Event is gone by design — resolve the Calendar from the
	// snapshot instead and raise its Source, the only surface left (#292).
	if msg.Method == repository.OutboxMethodDelete {
		if msg.WriteBackDelete == nil {
			return nil
		}
		calendar, err := s.calendars.GetByIDUnchecked(ctx, msg.WriteBackDelete.CalendarID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil
			}
			return fmt.Errorf("load calendar for terminal write-back failure: %w", err)
		}
		return s.markWriteBackPermanentlyFailed(ctx, calendar.UserID, msg.EventID, calendar.ID,
			fmt.Sprintf("could not delete this event at google: %v", sendErr))
	}

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
