// attachment.go implements Attachments' web-app-facing REST surface (#132,
// ADR-0040): upload, download, and delete under /api/events/{id}/attachments —
// list is not its own endpoint, since an Event's Attachments already ride
// along on every GET/List of the Event itself (event.go's eventResponse).
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

type AttachmentHandler struct {
	attachments *service.AttachmentService
	// maxUploadBytes caps the whole multipart request at MAX_ATTACHMENT_SIZE,
	// enforced via http.MaxBytesReader exactly as ics_import.go's Import
	// does — an oversized upload is rejected while still streaming in,
	// before any byte reaches disk (ADR-0040).
	maxUploadBytes int64
}

func NewAttachmentHandler(attachments *service.AttachmentService, maxAttachmentSize int64) *AttachmentHandler {
	return &AttachmentHandler{attachments: attachments, maxUploadBytes: maxAttachmentSize}
}

var attachmentWriteErrors = []errorCase{
	{service.ErrAttachmentOnOverride, badRequest("an attachment must be added to a master event, not an override")},
	{service.ErrTooManyAttachments, badRequest("event already has the maximum number of attachments")},
	{repository.ErrNotFound, notFound("event not found")},
	{service.ErrCalendarReadOnly, forbidden("calendar is read-only")},
}

var attachmentNotFoundErrors = []errorCase{
	{repository.ErrNotFound, notFound("attachment not found")},
}

var attachmentDeleteErrors = alsoHandling(attachmentNotFoundErrors,
	errorCase{service.ErrCalendarReadOnly, forbidden("calendar is read-only")},
)

// Upload serves POST /api/events/{id}/attachments: a multipart upload
// carrying a single "file" part.
func (h *AttachmentHandler) Upload(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	eventID := chi.URLParam(r, "id")

	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be a multipart/form-data upload no larger than the configured attachment size limit")
		return
	}
	// ParseMultipartForm spills a large "file" part to an OS temp file
	// rather than holding it in memory (its 32MB threshold above); this
	// reclaims that temp file once the request is done, since our own copy
	// already lives under attachmentstore by then.
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", `a "file" part is required`)
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	attachment, err := h.attachments.Upload(r.Context(), userID, eventID, header.Filename, contentType, file)
	if respondError(w, err, attachmentWriteErrors, "failed to upload attachment") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toAttachmentResponse(attachment))
}

// Download serves GET /api/events/{id}/attachments/{attachmentId} — see
// httpresponse.Attachment for the headers ADR-0040 requires on every
// response.
func (h *AttachmentHandler) Download(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	attachmentID := chi.URLParam(r, "attachmentId")

	attachment, file, err := h.attachments.Download(r.Context(), userID, attachmentID)
	if respondError(w, err, attachmentNotFoundErrors, "failed to download attachment") {
		return
	}
	defer file.Close()

	httpresponse.Attachment(w, attachment.ContentType, attachment.Filename, attachment.SizeBytes, file)
}

// Delete serves DELETE /api/events/{id}/attachments/{attachmentId} — no
// confirmation step server-side; the web app's inline remove is the only
// confirmation this action gets (ADR-0040).
func (h *AttachmentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	attachmentID := chi.URLParam(r, "attachmentId")

	err := h.attachments.Delete(r.Context(), userID, attachmentID)
	if respondError(w, err, attachmentDeleteErrors, "failed to delete attachment") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
