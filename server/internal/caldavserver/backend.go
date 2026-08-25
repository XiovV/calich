// Package caldavserver implements emersion/go-webdav's caldav.Backend
// interface (ADR-0023) over this app's existing CalendarService and
// EventService, so a native calendar client can discover a User's Calendars
// as CalDAV collections and read their Events as calendar objects (#64).
//
// go-webdav's Calendar type has no field for the Apple/DAVx⁵ calendar-color
// extension and the library gives Backend no hook to inject extra PROPFIND
// XML, so exposing/accepting it is done by XML-patching outside the library
// (color.go, propfind.go, proppatch.go — ADR-0028).
package caldavserver

import (
	"context"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/service"
)

// chi only routes its own fixed set of HTTP methods by default; CalDAV
// clients also send PROPFIND/REPORT/MKCOL/PROPPATCH, so those must be
// registered before any router mounts CalDAV routes (ADR-0023, ADR-0028).
func init() {
	chi.RegisterMethod("PROPFIND")
	chi.RegisterMethod("REPORT")
	chi.RegisterMethod("MKCOL")
	chi.RegisterMethod("PROPPATCH")
}

type Backend struct {
	calendars   *service.CalendarService
	events      *service.EventService
	attachments *service.AttachmentService
	// maxAttachmentSize and maxAttachmentsPerEvent are Attachments' limits
	// (#132, ADR-0040), advertised on the calendar collection as RFC 8607's
	// max-attachment-size/max-attachments-per-resource (propfind.go) and
	// enforced against an attachment-add/-update body (attachment_actions.go).
	maxAttachmentSize      int64
	maxAttachmentsPerEvent int
}

func NewBackend(calendars *service.CalendarService, events *service.EventService, attachments *service.AttachmentService, maxAttachmentSize int64, maxAttachmentsPerEvent int) *Backend {
	return &Backend{calendars: calendars, events: events, attachments: attachments, maxAttachmentSize: maxAttachmentSize, maxAttachmentsPerEvent: maxAttachmentsPerEvent}
}

func (b *Backend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return "", err
	}
	return principalPath(userID), nil
}

func (b *Backend) CalendarHomeSetPath(ctx context.Context) (string, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return "", err
	}
	return homeSetPath(userID), nil
}
