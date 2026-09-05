package caldavserver

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/emersion/go-webdav/caldav"
)

// dispatchHandler wraps go-webdav's caldav.Handler with the extensions it
// doesn't support: the sync-collection REPORT (sync.go), the PROPFIND
// properties go-webdav has no hook for — getctag (ctag.go) and calendar-color
// (color.go), both applied by propfind.go's single-pass dispatcher —
// PROPPATCH (proppatch.go, since go-webdav's caldav.Backend has no PropPatch
// hook at all and its internal implementation unconditionally 501s), and
// forwarding DELETE's If-Match header through to
// Backend.DeleteCalendarObject (whose signature, unlike PutCalendarObject's,
// carries no options — see go-webdav's caldav.Backend interface; #67 needs
// If-Match honored on DELETE regardless). All of these need to inspect a
// request or response the library itself owns, so they're implemented by
// intercepting at the HTTP layer rather than through caldav.Backend.
type dispatchHandler struct {
	base    http.Handler
	backend *Backend
}

type ifMatchContextKey struct{}

// ifMatchFromContext returns the If-Match header dispatchHandler stashed
// for a DELETE request, if any (see ifMatchContextKey's doc comment above).
func ifMatchFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ifMatchContextKey{}).(string)
	return v, ok
}

type baseURLContextKey struct{}

// baseURLFromContext returns the scheme+host string dispatchHandler derived
// from the incoming request (see RequestBaseURL), if any.
func baseURLFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(baseURLContextKey{}).(string)
	return v, ok
}

// RequestBaseURL derives the scheme and host a client outside this process
// would use to reach it, so an ATTACH property (a plain-text URI with no
// resolution context of its own, unlike a WebDAV href — #142) can be built
// as a fully-qualified URL. X-Forwarded-Proto/X-Forwarded-Host take
// precedence when present, for a reverse-proxied deployment; otherwise the
// request's own TLS state and Host header apply. Exported: handlers.
// ConnectionHandler (#285) derives its OAuth redirect_uri the same way, and
// shares this rather than a second copy of the same proxy-trust logic.
func RequestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}

	host := r.Host
	if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}

	return scheme + "://" + host
}

// NewHTTPHandler returns the http.Handler that serves everything under
// pathPrefix: base CalDAV (discovery, GET/PUT/DELETE, calendar-query and
// calendar-multiget REPORTs) via go-webdav, plus sync-collection, getctag,
// calendar-color, PROPPATCH, and RFC 8607 managed attachments on top
// (ADR-0023, ADR-0025, ADR-0028, ADR-0040, #65, #133).
func NewHTTPHandler(backend *Backend) http.Handler {
	return &dispatchHandler{
		base:    &caldav.Handler{Backend: backend, Prefix: pathPrefix},
		backend: backend,
	}
}

func (h *dispatchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), baseURLContextKey{}, RequestBaseURL(r)))

	// GET /dav/attachments/{managed-id} lives outside go-webdav's own
	// resource tree entirely (it isn't a calendar, a calendar object, or a
	// principal), so it's matched on its own reserved path prefix before the
	// method switch below even runs (#133, ADR-0040).
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, attachmentsBasePath) {
		h.serveAttachmentDownload(w, r)
		return
	}

	switch r.Method {
	case "REPORT":
		body, err := readAndRestoreBody(r)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		if isSyncCollectionRequest(body) {
			h.handleSyncCollection(w, r, body)
			return
		}
	case "PROPFIND":
		h.servePropfind(w, r)
		return
	case "PROPPATCH":
		h.handlePropPatch(w, r)
		return
	case http.MethodPost:
		if r.URL.Query().Get("action") != "" {
			h.handlePostAction(w, r)
			return
		}
	case http.MethodDelete:
		if ifMatch := r.Header.Get("If-Match"); ifMatch != "" {
			ctx := context.WithValue(r.Context(), ifMatchContextKey{}, ifMatch)
			r = r.WithContext(ctx)
		}
	}

	h.base.ServeHTTP(w, r)
}

// readAndRestoreBody reads r.Body in full and replaces it with a fresh
// reader over the same bytes, so a caller that only needs to peek at the
// body doesn't consume it out from under whichever handler serves the
// request next.
func readAndRestoreBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}
