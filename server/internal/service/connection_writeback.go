// connection_writeback.go implements Write-back's send side (#290,
// ADR-0075): draining one queued OutboxKindWriteBack message by rebuilding
// the push from the Event's own live state and PATCHing it to Google, then
// applying the response as though it were a Refresh result (ADR-0076).
//
// Rebuilt from live state, never a stored snapshot, mirroring
// InvitationSender.sendInvitation's own contract (invitation.go) rather than
// its sendCancellation one: a push that sends late still reflects whatever
// the Event looks like right now, including a second edit made before the
// first push ever went out.
package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/XiovV/calich/server/internal/repository"
)

// SendWriteBack drains one OutboxKindWriteBack message (#290, ADR-0075): it
// resolves msg.EventID's current row and Calendar, and does nothing — a
// success, not a failure, so the Worker marks msg sent — whenever there is
// genuinely nothing left to push: the Event was deleted since msg was
// queued, it never reached the Provider in the first place (ExternalUID
// nil — Create's own write-back is #292's), its Calendar is no longer a
// writable Linked Calendar (moved elsewhere, unshared, or the Provider
// revoked write access since), or its Connection was disconnected. Every one
// of these is a real outcome a background push can race against, not a bug
// to guard defensively against; none of them should retry forever for a
// state that will never resolve.
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

	updated, err := s.google.patchEvent(ctx, accessToken, *calendar.Source.ExternalCalendarID, *event.ExternalUID, event.ProviderEtag, patch)
	if isGoogleAccessTokenExpired(err) {
		accessToken, err = s.mintAccessToken(ctx, calendar.UserID, conn)
		if err != nil {
			return err
		}
		updated, err = s.google.patchEvent(ctx, accessToken, *calendar.Source.ExternalCalendarID, *event.ExternalUID, event.ProviderEtag, patch)
	}
	if err != nil {
		return err
	}

	// Echo suppression (ADR-0075, ADR-0076): see RecordWriteBackEtag's own
	// doc comment.
	return s.events.RecordWriteBackEtag(ctx, event.ID, googleEtag(updated.ETag))
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
