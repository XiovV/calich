// attachment_actions.go implements RFC 8607's three managed-attachment POST
// actions (attachment-add/-update/-remove) on a calendar object resource,
// plus the GET route that serves an Attachment's bytes back to a CalDAV
// client (#133, ADR-0040). go-webdav's caldav.Handler has no hook for either
// — it doesn't route POST at all, and the download route addresses no
// calendar object go-webdav knows about — so both are implemented by
// intercepting at the HTTP layer, the same technique sync.go and
// proppatch.go use for their own unsupported methods (handler.go).
package caldavserver

import (
	"context"
	"errors"
	"mime"
	"net/http"

	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// ridSeriesToken is RFC 8607's rid=M — "the master instance", which this app
// treats as a no-op alias for rid absent, since storage is Master-only
// (#132). Any other rid names a specific RECURRENCE-ID, which this app
// deliberately rejects rather than silently applying to the whole series —
// see validateRid.
const ridSeriesToken = "M"

var errInvalidRid = errors.New("rid must be absent or \"M\": a specific RECURRENCE-ID is not supported, since an attachment belongs to the whole series (ADR-0040)")

// validateRid enforces ADR-0040's rid resolution: absent is always fine (it
// means "the whole series", the common client default); "M" is fine only
// where allowSeriesToken says so — RFC 8607 forbids rid on attachment-update
// entirely, since managed-id alone already names one Attachment unambiguously.
// Anything else — a specific RECURRENCE-ID — is the one deliberate deviation
// this app makes: rejected outright rather than silently applied to the
// series (documented in ADR-0040 and issue #133).
func validateRid(rid string, allowSeriesToken bool) error {
	if rid == "" {
		return nil
	}
	if allowSeriesToken && rid == ridSeriesToken {
		return nil
	}
	return errInvalidRid
}

// parseAttachmentUpload reads a POST action's Content-Type and
// Content-Disposition headers into the (filename, contentType) pair
// AttachmentService.Upload/Replace store — RFC 8607's body is the raw file
// itself, not a multipart upload (unlike the REST API's, handlers/attachment.go).
func parseAttachmentUpload(r *http.Request) (filename, contentType string) {
	contentType = r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	if cd := r.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			filename = params["filename"]
		}
	}
	if filename == "" {
		filename = "attachment"
	}
	return filename, contentType
}

// postActionErrorCase pairs an AttachmentService sentinel with its rendering
// — an ordered slice rather than a map so matching order is deterministic,
// the same table-driven shape handlers/errors.go's errorCase uses for this
// package's REST sibling (attachmentWriteErrors et al. in
// handlers/attachment.go). Not the same type: that one renders a JSON body
// via httpresponse.Error, plain text is the right shape for a CalDAV client,
// and errorCase itself is unexported from a different package — but the
// dispatch shape (an ordered table, first match wins) is deliberately the
// same rather than an ad hoc switch.
type postActionErrorCase struct {
	err     error
	status  int
	message string
}

var attachmentPostActionErrors = []postActionErrorCase{
	{repository.ErrNotFound, http.StatusNotFound, "calendar object or attachment not found"},
	{service.ErrCalendarReadOnly, http.StatusForbidden, "calendar is read-only"},
	{service.ErrAttachmentOnOverride, http.StatusBadRequest, service.ErrAttachmentOnOverride.Error()},
	{service.ErrTooManyAttachments, http.StatusForbidden, service.ErrTooManyAttachments.Error()},
}

// writePostActionError maps an AttachmentService error onto the HTTP status
// an RFC 8607 client expects: a body over MAX_ATTACHMENT_SIZE is rejected
// before it ever reaches AttachmentService (http.MaxBytesReader, surfaced as
// *http.MaxBytesError once unwrapped) with the same 400 the REST API uses
// for the same condition (handlers/attachment.go).
func writePostActionError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		http.Error(w, "attachment exceeds the configured MAX_ATTACHMENT_SIZE", http.StatusBadRequest)
		return
	}
	for _, c := range attachmentPostActionErrors {
		if errors.Is(err, c.err) {
			http.Error(w, c.message, c.status)
			return
		}
	}
	http.Error(w, "failed to process attachment action", http.StatusInternalServerError)
}

// respondAttachmentMutation writes the response shape attachment-add and
// attachment-update share once their underlying AttachmentService call has
// already succeeded: Cal-Managed-ID, Location, a freshly recomputed ETag,
// and 201 (RFC 8607 defines both actions as responding 201).
func (h *dispatchHandler) respondAttachmentMutation(w http.ResponseWriter, ctx context.Context, userID int64, calendarID, masterID string, attachment repository.Attachment) {
	_, etag, err := h.backend.currentObjectETag(ctx, userID, calendarID, masterID)
	if err != nil {
		http.Error(w, "failed to compute updated etag", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cal-Managed-ID", attachment.ID)
	w.Header().Set("Location", attachmentDownloadPath(attachment.ID))
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusCreated)
}

// handlePostAction dispatches a calendar object resource's ?action= query —
// attachment-add, attachment-update, or attachment-remove — after resolving
// the resource path and validating rid. Every other query value is a 400:
// this app implements no other POST action on a calendar object.
func (h *dispatchHandler) handlePostAction(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromContext(r.Context())
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	calendarID, masterID, err := calendarObjectIDFromPath(userID, r.URL.Path)
	if err != nil {
		http.Error(w, "calendar object not found", http.StatusNotFound)
		return
	}

	exists, _, err := h.backend.currentObjectETag(r.Context(), userID, calendarID, masterID)
	if err != nil {
		http.Error(w, "failed to resolve calendar object", http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "calendar object not found", http.StatusNotFound)
		return
	}

	rid := r.URL.Query().Get("rid")

	switch r.URL.Query().Get("action") {
	case "attachment-add":
		if err := validateRid(rid, true); err != nil {
			http.Error(w, err.Error(), http.StatusPreconditionFailed)
			return
		}
		h.handleAttachmentAdd(w, r, userID, calendarID, masterID)
	case "attachment-update":
		if err := validateRid(rid, false); err != nil {
			http.Error(w, err.Error(), http.StatusPreconditionFailed)
			return
		}
		h.handleAttachmentUpdate(w, r, userID, calendarID, masterID, r.URL.Query().Get("managed-id"))
	case "attachment-remove":
		if err := validateRid(rid, true); err != nil {
			http.Error(w, err.Error(), http.StatusPreconditionFailed)
			return
		}
		h.handleAttachmentRemove(w, r, userID, r.URL.Query().Get("managed-id"))
	default:
		http.Error(w, "unknown or missing action", http.StatusBadRequest)
	}
}

// handleAttachmentAdd serves ?action=attachment-add: 201 with Cal-Managed-ID,
// Location and the object's fresh ETag on success.
func (h *dispatchHandler) handleAttachmentAdd(w http.ResponseWriter, r *http.Request, userID int64, calendarID, masterID string) {
	filename, contentType := parseAttachmentUpload(r)
	r.Body = http.MaxBytesReader(w, r.Body, h.backend.maxAttachmentSize)

	attachment, err := h.backend.attachments.Upload(r.Context(), userID, masterID, filename, contentType, r.Body)
	if err != nil {
		writePostActionError(w, err)
		return
	}

	h.respondAttachmentMutation(w, r.Context(), userID, calendarID, masterID, attachment)
}

// handleAttachmentUpdate serves ?action=attachment-update&managed-id=<id>:
// same response shape as handleAttachmentAdd — RFC 8607 defines this action
// as also responding 201.
func (h *dispatchHandler) handleAttachmentUpdate(w http.ResponseWriter, r *http.Request, userID int64, calendarID, masterID, managedID string) {
	if managedID == "" {
		http.Error(w, "managed-id is required", http.StatusBadRequest)
		return
	}

	filename, contentType := parseAttachmentUpload(r)
	r.Body = http.MaxBytesReader(w, r.Body, h.backend.maxAttachmentSize)

	attachment, err := h.backend.attachments.Replace(r.Context(), userID, managedID, filename, contentType, r.Body)
	if err != nil {
		writePostActionError(w, err)
		return
	}

	h.respondAttachmentMutation(w, r.Context(), userID, calendarID, masterID, attachment)
}

// handleAttachmentRemove serves ?action=attachment-remove&managed-id=<id>:
// 204 on success, with no body to carry a fresh ETag in (a client re-fetches
// the object, as it would after any other DELETE/PUT that changes it).
func (h *dispatchHandler) handleAttachmentRemove(w http.ResponseWriter, r *http.Request, userID int64, managedID string) {
	if managedID == "" {
		http.Error(w, "managed-id is required", http.StatusBadRequest)
		return
	}

	if err := h.backend.attachments.Delete(r.Context(), userID, managedID); err != nil {
		writePostActionError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// serveAttachmentDownload serves GET /dav/attachments/{managed-id}: the same
// storage read and the same three security headers as the REST API's
// download route (handlers/attachment.go's Download), authenticated via
// RequireCalDAVAuth (Basic over App password, ADR-0024) like everything else
// under /dav rather than a hybrid Bearer+Basic authenticator (#133, ADR-0040).
func (h *dispatchHandler) serveAttachmentDownload(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromContext(r.Context())
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	managedID := r.URL.Path[len(attachmentsBasePath):]
	if managedID == "" {
		http.NotFound(w, r)
		return
	}

	attachment, file, err := h.backend.attachments.Download(r.Context(), userID, managedID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to download attachment", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	httpresponse.Attachment(w, attachment.ContentType, attachment.Filename, attachment.SizeBytes, file)
}
