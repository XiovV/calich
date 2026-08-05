package caldavserver

import (
	"bytes"
	"context"
	"io"
	"net/http"

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

// NewHTTPHandler returns the http.Handler that serves everything under
// pathPrefix: base CalDAV (discovery, GET/PUT/DELETE, calendar-query and
// calendar-multiget REPORTs) via go-webdav, plus sync-collection, getctag,
// calendar-color, and PROPPATCH on top (ADR-0023, ADR-0025, ADR-0028, #65).
func NewHTTPHandler(backend *Backend) http.Handler {
	return &dispatchHandler{
		base:    &caldav.Handler{Backend: backend, Prefix: pathPrefix},
		backend: backend,
	}
}

func (h *dispatchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
