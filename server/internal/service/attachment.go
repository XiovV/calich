// attachment.go implements Attachments (#132, ADR-0040): a file held
// against a Master, shown on every Occurrence of its series. It is a
// sibling of EventService rather than a method on it, since its own
// dependencies (the blob store, the size/count limits) have nothing to do
// with recurrence or sync.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/attachmentstore"
	"github.com/XiovV/calich/server/internal/repository"
)

var (
	// ErrAttachmentOnOverride is returned when an Attachment is added to an
	// Override rather than a Master — deliberately unrepresentable (ADR-0040):
	// an Attachment belongs to the series, not to one Occurrence.
	ErrAttachmentOnOverride = errors.New("an attachment must be added to a master event, not an override")
	// ErrTooManyAttachments is returned when an Event already carries
	// MAX_ATTACHMENTS_PER_EVENT Attachments.
	ErrTooManyAttachments = errors.New("event already has the maximum number of attachments")
)

// AttachmentService is Attachments' write and read path (#132, ADR-0040):
// every method resolves Access through the Attachment's Event's Calendar —
// ADR-0034 unchanged, no permission path of its own.
type AttachmentService struct {
	attachments *repository.AttachmentRepository
	events      *repository.EventRepository
	calendars   *CalendarService
	// eventService bumps a Master's change_seq after every add/replace/remove
	// (TouchChangeSeq), so the Master's ETag and its Calendar's CTag reflect
	// an Attachment change the same way they reflect any other write to the
	// series (#133, ADR-0040) — this covers both this service's REST callers
	// and CalDAV's RFC 8607 POST actions, which both funnel through here.
	eventService *EventService
	store        *attachmentstore.Store
	maxPerEvent  int
}

func NewAttachmentService(attachments *repository.AttachmentRepository, events *repository.EventRepository, calendars *CalendarService, eventService *EventService, store *attachmentstore.Store, maxPerEvent int) *AttachmentService {
	return &AttachmentService{attachments: attachments, events: events, calendars: calendars, eventService: eventService, store: store, maxPerEvent: maxPerEvent}
}

// requireAccess resolves userID's Access to calendarID and refuses it
// unless CanRead — repository.ErrNotFound both when calendarID doesn't
// resolve and when the caller simply can't see it, matching
// EventService.getOwnedEvent so a stranger can't tell "no such event" apart
// from "not yours to see" — and, when needWrite, unless CanWrite too
// (Owner/Editor add and remove; Viewer only downloads, ADR-0034).
func (s *AttachmentService) requireAccess(ctx context.Context, userID int64, calendarID string, needWrite bool) error {
	access, _, err := s.calendars.Access(ctx, userID, calendarID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.ErrNotFound
		}
		return fmt.Errorf("resolve calendar access: %w", err)
	}
	if !access.CanRead() {
		return repository.ErrNotFound
	}
	if needWrite && !access.CanWrite() {
		return ErrCalendarReadOnly
	}
	return nil
}

// eventForAttachment resolves eventID and checks the caller's Access to its
// Calendar, mirroring EventService.getOwnedEvent.
func (s *AttachmentService) eventForAttachment(ctx context.Context, userID int64, eventID string, needWrite bool) (repository.Event, error) {
	event, err := s.events.GetByID(ctx, eventID)
	if err != nil {
		return repository.Event{}, err
	}
	if err := s.requireAccess(ctx, userID, event.CalendarID, needWrite); err != nil {
		return repository.Event{}, err
	}
	return event, nil
}

// Upload adds a new Attachment to eventID, streaming r to disk before its
// row commits (ADR-0040's crash ordering) — a write failure after the file
// is saved rolls the file back immediately rather than waiting for a Sweep,
// since here (unlike an actual crash) there is a chance to.
func (s *AttachmentService) Upload(ctx context.Context, userID int64, eventID, filename, contentType string, r io.Reader) (repository.Attachment, error) {
	event, err := s.eventForAttachment(ctx, userID, eventID, true)
	if err != nil {
		return repository.Attachment{}, err
	}
	if event.ParentID != nil {
		return repository.Attachment{}, ErrAttachmentOnOverride
	}

	count, err := s.attachments.CountByEventID(ctx, eventID)
	if err != nil {
		return repository.Attachment{}, fmt.Errorf("count attachments: %w", err)
	}
	if count >= s.maxPerEvent {
		return repository.Attachment{}, ErrTooManyAttachments
	}

	id := uuid.NewString()
	size, err := s.store.Save(id, r)
	if err != nil {
		return repository.Attachment{}, fmt.Errorf("save attachment: %w", err)
	}

	created, err := s.attachments.Create(ctx, id, eventID, &userID, filename, contentType, size)
	if err != nil {
		s.store.Delete(id)
		return repository.Attachment{}, fmt.Errorf("insert attachment: %w", err)
	}

	if err := s.eventService.TouchChangeSeq(ctx, eventID); err != nil {
		return repository.Attachment{}, fmt.Errorf("bump change seq: %w", err)
	}
	return created, nil
}

// Replace overwrites attachmentID's bytes and metadata in place — RFC 8607's
// attachment-update action (#133, ADR-0040): the MANAGED-ID, and therefore
// every ATTACH referencing it, is unchanged, only the file underneath. Like
// Upload, the new bytes are saved before the row updates, so a write failure
// after the save rolls back immediately rather than waiting for a Sweep.
func (s *AttachmentService) Replace(ctx context.Context, userID int64, attachmentID, filename, contentType string, r io.Reader) (repository.Attachment, error) {
	attachment, err := s.attachments.GetByID(ctx, attachmentID)
	if err != nil {
		return repository.Attachment{}, err
	}
	if _, err := s.eventForAttachment(ctx, userID, attachment.EventID, true); err != nil {
		return repository.Attachment{}, err
	}

	size, err := s.store.Save(attachmentID, r)
	if err != nil {
		return repository.Attachment{}, fmt.Errorf("save attachment: %w", err)
	}

	updated, err := s.attachments.Update(ctx, attachmentID, filename, contentType, size)
	if err != nil {
		return repository.Attachment{}, fmt.Errorf("update attachment: %w", err)
	}

	if err := s.eventService.TouchChangeSeq(ctx, attachment.EventID); err != nil {
		return repository.Attachment{}, fmt.Errorf("bump change seq: %w", err)
	}
	return updated, nil
}

// Delete removes attachmentID: the row first, then its file best-effort
// (ADR-0040) — a failure unlinking never leaves an orphaned row, only an
// orphaned file, which Sweep reclaims.
func (s *AttachmentService) Delete(ctx context.Context, userID int64, attachmentID string) error {
	attachment, err := s.attachments.GetByID(ctx, attachmentID)
	if err != nil {
		return err
	}
	if _, err := s.eventForAttachment(ctx, userID, attachment.EventID, true); err != nil {
		return err
	}

	if err := s.attachments.Delete(ctx, attachmentID); err != nil {
		return err
	}
	s.store.Delete(attachmentID)

	if err := s.eventService.TouchChangeSeq(ctx, attachment.EventID); err != nil {
		return fmt.Errorf("bump change seq: %w", err)
	}
	return nil
}

// Download resolves attachmentID for a caller with at least Viewer Access
// and returns its metadata alongside an open handle to its bytes; the
// caller must Close it.
func (s *AttachmentService) Download(ctx context.Context, userID int64, attachmentID string) (repository.Attachment, *os.File, error) {
	attachment, err := s.attachments.GetByID(ctx, attachmentID)
	if err != nil {
		return repository.Attachment{}, nil, err
	}
	if _, err := s.eventForAttachment(ctx, userID, attachment.EventID, false); err != nil {
		return repository.Attachment{}, nil, err
	}

	f, err := s.store.Open(attachmentID)
	if err != nil {
		return repository.Attachment{}, nil, fmt.Errorf("open attachment: %w", err)
	}
	return attachment, f, nil
}
