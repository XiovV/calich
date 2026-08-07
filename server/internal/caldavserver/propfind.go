// propfind.go dispatches a PROPFIND request through exactly one recorder
// pass, then applies whichever of propfindPatches the request body actually
// asks for, in order, over the same evolving bytes — needed because a real
// client (macOS Calendar, DAVx⁵) typically asks for getctag and
// calendar-color in the same PROPFIND body, and each patch must not
// independently re-run the request or write to the response (ADR-0025,
// ADR-0028, ADR-0029).
package caldavserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
)

// propfindPatch is one extension property's optional pass over a recorded
// PROPFIND response: trigger is a cheap substring probe (checked before
// paying for a recorder pass at all); apply performs the actual rewrite.
type propfindPatch struct {
	trigger string
	apply   func(ctx context.Context, h *dispatchHandler, userID int64, body []byte) []byte
}

var propfindPatches = []propfindPatch{
	{trigger: "getctag", apply: applyGetCTagPatch},
	{trigger: "calendar-color", apply: applyCalendarColorPatch},
	{trigger: "current-user-privilege-set", apply: applyPrivilegeSetPatch},
}

// calendarColorNamespace is the Apple/DAVx⁵ calendar-color extension's
// namespace (ADR-0028).
const calendarColorNamespace = "http://apple.com/ns/ical/"

// collectionValueFunc adapts a per-Calendar lookup into a propertyValueFunc:
// only an href that is a Calendar collection directly under the user's
// calendar-home-set (a trailing "/", and calendarIDFromPath succeeding)
// gets a value; everything else (the home-set itself, the principal,
// calendar-object rows) is left alone.
func collectionValueFunc(userID int64, lookup func(ctx context.Context, calendarID string) (string, bool)) propertyValueFunc {
	return func(ctx context.Context, href string) (string, bool) {
		if !strings.HasSuffix(href, "/") {
			return "", false
		}
		calendarID, err := calendarIDFromPath(userID, href)
		if err != nil {
			return "", false
		}
		return lookup(ctx, calendarID)
	}
}

func applyGetCTagPatch(ctx context.Context, h *dispatchHandler, userID int64, body []byte) []byte {
	return injectGetCTag(ctx, body, collectionValueFunc(userID, func(ctx context.Context, calendarID string) (string, bool) {
		ctag, err := h.backend.events.CalendarCTag(ctx, userID, calendarID)
		if err != nil {
			return "", false
		}
		return strconv.FormatInt(ctag, 10), true
	}))
}

// applyCalendarColorPatch serves a Calendar's color exactly as stored — the
// Color column is already the canonical "#RRGGBBAA" hex (ADR-0029), so
// there's no enum-to-hex lookup left to do here.
func applyCalendarColorPatch(ctx context.Context, h *dispatchHandler, userID int64, body []byte) []byte {
	return injectProperty(ctx, body, "calendar-color", calendarColorNamespace, collectionValueFunc(userID, func(ctx context.Context, calendarID string) (string, bool) {
		cal, err := h.backend.calendars.Get(ctx, userID, calendarID)
		if err != nil {
			return "", false
		}
		return cal.Color, true
	}))
}

// subscribedReadOnlyPrivilegeSet is the current-user-privilege-set value
// (RFC 3744 §5.4) advertised for a Subscribed Calendar's collection: read
// without write, so a client whose privilege-set support is good enough to
// act on it stops offering edits that can only be refused (ADR-0032, #89).
const subscribedReadOnlyPrivilegeSet = `<privilege><read/></privilege>`

// applyPrivilegeSetPatch overrides go-webdav's hardcoded read+write
// current-user-privilege-set (caldav/server.go's propFindCalendar) to read
// without write whenever the caller's Access to the collection isn't
// writable — a Subscribed Calendar (ADR-0032) or a Viewer Share (ADR-0034,
// ADR-0035) alike — declining (ok=false) for anything writable leaves the
// library's default untouched.
func applyPrivilegeSetPatch(ctx context.Context, h *dispatchHandler, userID int64, body []byte) []byte {
	return injectPropertyRaw(ctx, body, "current-user-privilege-set", davNamespace, collectionValueFunc(userID, func(ctx context.Context, calendarID string) (string, bool) {
		access, _, err := h.backend.calendars.Access(ctx, userID, calendarID)
		if err != nil || access.CanWrite() {
			return "", false
		}
		return subscribedReadOnlyPrivilegeSet, true
	}))
}

// servePropfind runs a PROPFIND through h.base exactly once and, for each
// propfindPatch whose trigger appears in the request body, applies its
// rewrite in propfindPatches' order — each pass over the previous pass's
// already-edited bytes — then writes the final result exactly once. A
// request mentioning none of them passes through untouched.
func (h *dispatchHandler) servePropfind(w http.ResponseWriter, r *http.Request) {
	body, err := readAndRestoreBody(r)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	var active []propfindPatch
	for _, p := range propfindPatches {
		if bytes.Contains(body, []byte(p.trigger)) {
			active = append(active, p)
		}
	}
	if len(active) == 0 {
		h.base.ServeHTTP(w, r)
		return
	}

	userID, err := userIDFromContext(r.Context())
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	rec := httptest.NewRecorder()
	h.base.ServeHTTP(rec, r)

	patched := rec.Body.Bytes()
	for _, p := range active {
		patched = p.apply(r.Context(), h, userID, patched)
	}

	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.Code)
	w.Write(patched)
}
