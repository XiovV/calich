// sync.go implements the sync-collection REPORT (RFC 6578) go-webdav's
// caldav.Handler doesn't support (its handleReport only recognizes
// calendar-query and calendar-multiget) — the incremental-sync half of
// ADR-0025/#65. It runs entirely outside the library: dispatchHandler
// intercepts REPORT requests carrying a <sync-collection> body before they
// ever reach caldav.Handler.
package caldavserver

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/emersion/go-ical"
)

// syncTokenPrefix makes a sync-token look like the opaque URI RFC 6578
// expects, while staying trivially parseable back into the change_seq
// high-water mark it wraps (ADR-0025).
const syncTokenPrefix = "urn:x-caldav-sync:"

func formatSyncToken(changeSeq int64) string {
	return syncTokenPrefix + strconv.FormatInt(changeSeq, 10)
}

// parseSyncToken parses a sync-token back into its change_seq high-water
// mark. An empty token (an initial sync — RFC 6578 §3.7) parses as 0, so
// every live series is reported.
func parseSyncToken(token string) (int64, error) {
	if token == "" {
		return 0, nil
	}
	rest, ok := strings.CutPrefix(token, syncTokenPrefix)
	if !ok {
		return 0, fmt.Errorf("unrecognized sync-token %q", token)
	}
	seq, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("malformed sync-token %q: %w", token, err)
	}
	return seq, nil
}

// syncCollectionRequest is the REPORT body RFC 6578 §3.7 defines. Only
// sync-token and whether calendar-data was requested matter here — every
// object is always reported with its full VCALENDAR when it is.
type syncCollectionRequest struct {
	XMLName   xml.Name `xml:"DAV: sync-collection"`
	SyncToken string   `xml:"DAV: sync-token"`
	Prop      *struct {
		CalendarData *struct{} `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
	} `xml:"DAV: prop"`
}

// isSyncCollectionRequest reports whether body's root element is
// <sync-collection>, distinguishing it from the calendar-query/multiget
// REPORT bodies go-webdav's Handler already handles.
func isSyncCollectionRequest(body []byte) bool {
	var probe struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(body, &probe); err != nil {
		return false
	}
	return probe.XMLName.Local == "sync-collection"
}

// syncMultistatus is RFC 6578 §3.7's response: a multistatus of changed and
// removed objects, plus the sync-token to resume from next time.
type syncMultistatus struct {
	XMLName   xml.Name       `xml:"DAV: multistatus"`
	Responses []syncResponse `xml:"DAV: response"`
	SyncToken string         `xml:"DAV: sync-token"`
}

type syncResponse struct {
	Href     string        `xml:"DAV: href"`
	Propstat *syncPropstat `xml:"DAV: propstat,omitempty"`
	// Status is set directly (skipping Propstat) for a removed object: RFC
	// 6578 §3.7 reports a deletion as a bare 404 response, not a propstat.
	Status string `xml:"DAV: status,omitempty"`
}

type syncPropstat struct {
	Prop   syncProp `xml:"DAV: prop"`
	Status string   `xml:"DAV: status"`
}

type syncProp struct {
	GetETag      string `xml:"DAV: getetag"`
	CalendarData string `xml:"urn:ietf:params:xml:ns:caldav calendar-data,omitempty"`
}

// handleSyncCollection answers a sync-collection REPORT against the
// Calendar collection at r.URL.Path: every series changed since the
// request's sync-token, every series deleted since then as a 404 response,
// and the new sync-token to resume from (ADR-0025, #65).
func (h *dispatchHandler) handleSyncCollection(w http.ResponseWriter, r *http.Request, body []byte) {
	userID, err := userIDFromContext(r.Context())
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	calendarID, err := calendarIDFromPath(userID, r.URL.Path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var req syncCollectionRequest
	if err := xml.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid sync-collection request", http.StatusBadRequest)
		return
	}

	sinceToken, err := parseSyncToken(req.SyncToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	includeCalendarData := req.Prop != nil && req.Prop.CalendarData != nil

	result, err := h.backend.events.SyncSince(r.Context(), userID, calendarID, sinceToken)
	if err != nil {
		http.Error(w, "failed to compute sync diff", http.StatusInternalServerError)
		return
	}

	ms := syncMultistatus{SyncToken: formatSyncToken(result.NewToken)}
	for _, master := range result.Masters {
		object, err := buildCalendarObject(r.Context(), userID, calendarID, master, result.OverridesByParent[master.ID])
		if err != nil {
			http.Error(w, "failed to build calendar object", http.StatusInternalServerError)
			return
		}

		prop := syncProp{GetETag: fmt.Sprintf("%q", object.ETag)}
		if includeCalendarData {
			data, err := encodeICalendar(object.Data)
			if err != nil {
				http.Error(w, "failed to encode calendar object", http.StatusInternalServerError)
				return
			}
			prop.CalendarData = data
		}

		ms.Responses = append(ms.Responses, syncResponse{
			Href:     object.Path,
			Propstat: &syncPropstat{Prop: prop, Status: "HTTP/1.1 200 OK"},
		})
	}
	for _, uid := range result.DeletedUIDs {
		ms.Responses = append(ms.Responses, syncResponse{
			Href:   calendarObjectPath(userID, calendarID, uid),
			Status: "HTTP/1.1 404 Not Found",
		})
	}

	w.Header().Set("Content-Type", "application/xml; charset=\"utf-8\"")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(xml.Header))
	if err := xml.NewEncoder(w).Encode(ms); err != nil {
		return
	}
}

// encodeICalendar renders cal as the raw text a CalDAV calendar-data
// property carries.
func encodeICalendar(cal *ical.Calendar) (string, error) {
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", fmt.Errorf("encode calendar: %w", err)
	}
	return buf.String(), nil
}
