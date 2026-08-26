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
	"fmt"
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
	{trigger: "managed-attachments-server-URL", apply: applyManagedAttachmentsURLPatch},
	{trigger: "max-attachment-size", apply: applyMaxAttachmentSizePatch},
	{trigger: "max-attachments-per-resource", apply: applyMaxAttachmentsPerResourcePatch},
}

// calendarColorNamespace is the Apple/DAVx⁵ calendar-color extension's
// namespace (ADR-0028).
const calendarColorNamespace = "http://apple.com/ns/ical/"

// caldavNamespace is RFC 4791's CalDAV XML namespace, shared by every
// CalDAV-defined property this package injects (managed-attachments-server-URL,
// max-attachment-size, max-attachments-per-resource — #133, ADR-0040).
const caldavNamespace = "urn:ietf:params:xml:ns:caldav"

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
		if calendarID == attendeeCollectionID {
			ctag, err := h.backend.events.AttendeeOnlyCTag(ctx, userID)
			if err != nil {
				return "", false
			}
			return strconv.FormatInt(ctag, 10), true
		}
		ctag, err := h.backend.events.CalendarCTag(ctx, userID, calendarID)
		if err != nil {
			return "", false
		}
		return strconv.FormatInt(ctag, 10), true
	}))
}

// applyCalendarColorPatch serves userID's resolved display colour for the
// Calendar (ADR-0038's DisplayColor): their own override if they've set
// one, otherwise the Calendar's own stored colour, which is already the
// canonical "#RRGGBBAA" hex (ADR-0029) with no enum-to-hex lookup left to
// do. Resolving per-principal here — the same shape as
// applyPrivilegeSetPatch below — is what lets a shared Calendar show green
// to one caller and blue to another over CalDAV.
func applyCalendarColorPatch(ctx context.Context, h *dispatchHandler, userID int64, body []byte) []byte {
	return injectPropertyTreeOrUnchanged(ctx, body, "calendar-color", calendarColorNamespace, collectionValueFunc(userID, func(ctx context.Context, calendarID string) (string, bool) {
		accessResult, err := h.backend.calendars.AccessWithColor(ctx, userID, calendarID)
		if err != nil || !accessResult.Access.CanRead() {
			return "", false
		}
		return accessResult.Color, true
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
	return injectPropertyTreeRawOrUnchanged(ctx, body, "current-user-privilege-set", davNamespace, collectionValueFunc(userID, func(ctx context.Context, calendarID string) (string, bool) {
		// The synthetic Invitations collection (#163) has no
		// repository.Calendar row to resolve Access against — it is
		// always read-only, the same clamp a Subscribed Calendar or a
		// Viewer Share gets below.
		if calendarID == attendeeCollectionID {
			return subscribedReadOnlyPrivilegeSet, true
		}
		access, _, err := h.backend.calendars.Access(ctx, userID, calendarID)
		if err != nil || access.CanWrite() {
			return "", false
		}
		return subscribedReadOnlyPrivilegeSet, true
	}))
}

// homeSetValueFunc adapts a per-User lookup into a propertyValueFunc that
// only fires for the caller's own calendar-home-set href exactly — the
// scope managed-attachments-server-URL is advertised at (#133, ADR-0040) —
// leaving every other href (a Calendar collection, a calendar object, the
// principal) alone.
func homeSetValueFunc(userID int64, value func(ctx context.Context) (string, bool)) propertyValueFunc {
	target := homeSetPath(userID)
	return func(ctx context.Context, href string) (string, bool) {
		if href != target {
			return "", false
		}
		return value(ctx)
	}
}

// applyManagedAttachmentsURLPatch advertises CALDAV:managed-attachments-server-URL
// on the caller's calendar home collection as a path-only DAV:href (#133,
// ADR-0040): per RFC 8607, the client substitutes scheme and authority from
// its own request, so this server never needs to know its own external base
// URL.
func applyManagedAttachmentsURLPatch(ctx context.Context, h *dispatchHandler, userID int64, body []byte) []byte {
	return injectPropertyTreeRawOrUnchanged(ctx, body, "managed-attachments-server-URL", caldavNamespace, homeSetValueFunc(userID, func(ctx context.Context) (string, bool) {
		return fmt.Sprintf(`<href xmlns=%q>%s</href>`, davNamespace, attachmentsBasePath), true
	}))
}

// applyMaxAttachmentSizePatch and applyMaxAttachmentsPerResourcePatch
// advertise Attachments' MAX_ATTACHMENT_SIZE/MAX_ATTACHMENTS_PER_EVENT
// limits (config.go) on every Calendar collection the caller can at least
// read (#133, ADR-0040) — the same access gate applyCalendarColorPatch uses.
func applyMaxAttachmentSizePatch(ctx context.Context, h *dispatchHandler, userID int64, body []byte) []byte {
	return injectPropertyTreeOrUnchanged(ctx, body, "max-attachment-size", caldavNamespace, collectionValueFunc(userID, func(ctx context.Context, calendarID string) (string, bool) {
		access, _, err := h.backend.calendars.Access(ctx, userID, calendarID)
		if err != nil || !access.CanRead() {
			return "", false
		}
		return strconv.FormatInt(h.backend.maxAttachmentSize, 10), true
	}))
}

func applyMaxAttachmentsPerResourcePatch(ctx context.Context, h *dispatchHandler, userID int64, body []byte) []byte {
	return injectPropertyTreeOrUnchanged(ctx, body, "max-attachments-per-resource", caldavNamespace, collectionValueFunc(userID, func(ctx context.Context, calendarID string) (string, bool) {
		access, _, err := h.backend.calendars.Access(ctx, userID, calendarID)
		if err != nil || !access.CanRead() {
			return "", false
		}
		return strconv.Itoa(h.backend.maxAttachmentsPerEvent), true
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
