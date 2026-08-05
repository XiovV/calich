// proppatch.go implements PROPPATCH end-to-end outside go-webdav: the
// library's caldav.Backend interface has no PropPatch hook at all, and its
// private wrapper hardcodes PropPatch to always return 501 regardless of
// what any Backend supports (see handler.go's dispatch and backend.go's
// package doc). The only settable property this backend recognizes is
// Apple/DAVx⁵'s calendar-color (color.go, ADR-0028); every other named
// property, and any <remove> of calendar-color (the Color column is NOT
// NULL — there is no "unset"), is reported 403 Forbidden per property
// rather than silently ignored, so a client can tell its update didn't
// fully apply.
package caldavserver

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/XiovV/calendar/server/internal/repository"
)

const (
	proppatchStatusOK        = "HTTP/1.1 200 OK"
	proppatchStatusForbidden = "HTTP/1.1 403 Forbidden"
	proppatchStatusConflict  = "HTTP/1.1 409 Conflict"
	calendarColorLocalName   = "calendar-color"
)

var calendarColorPropName = xml.Name{Space: calendarColorNamespace, Local: calendarColorLocalName}

// proppatchPropElem captures one raw <prop> child generically — go-webdav's
// own internal.Prop/RawXMLValue types live in an unexported package of a
// different module and aren't importable here, so parsing is hand-rolled.
type proppatchPropElem struct {
	XMLName xml.Name
	Content string `xml:",chardata"`
}

// proppatchRequestBody is the RFC 4918 §14.19 <propertyupdate> body a
// PROPPATCH request carries.
type proppatchRequestBody struct {
	XMLName xml.Name `xml:"DAV: propertyupdate"`
	Set     []struct {
		Prop struct {
			Props []proppatchPropElem `xml:",any"`
		} `xml:"DAV: prop"`
	} `xml:"DAV: set"`
	Remove []struct {
		Prop struct {
			Props []proppatchPropElem `xml:",any"`
		} `xml:"DAV: prop"`
	} `xml:"DAV: remove"`
}

// proppatchPropResult is one named property's outcome; results sharing a
// Status are grouped into one propstat when building the response.
type proppatchPropResult struct {
	Name   xml.Name
	Status string
}

// handlePropPatch resolves the target Calendar collection, parses the
// request body, applies whichever instruction this backend supports, and
// writes a 207 Multi-Status response. A path that doesn't resolve to a
// Calendar collection the authenticated user owns is a bare 404 (matching
// sync.go's handleSyncCollection precedent for the same failure), not a
// multistatus body — the request never gets far enough to have per-property
// outcomes.
func (h *dispatchHandler) handlePropPatch(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromContext(r.Context())
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if !strings.HasSuffix(r.URL.Path, "/") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	calendarID, err := calendarIDFromPath(userID, r.URL.Path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	existing, err := h.backend.calendars.Get(r.Context(), userID, calendarID)
	if errors.Is(err, repository.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "failed to load calendar", http.StatusInternalServerError)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	var req proppatchRequestBody
	if err := xml.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid propertyupdate request", http.StatusBadRequest)
		return
	}

	results, err := h.applyPropPatch(r.Context(), userID, calendarID, existing, req)
	if err != nil {
		http.Error(w, "failed to apply property update", http.StatusInternalServerError)
		return
	}
	if len(results) == 0 {
		http.Error(w, "propertyupdate has no properties to update", http.StatusBadRequest)
		return
	}

	ms := buildPropPatchMultistatus(r.URL.Path, results)
	w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusMultiStatus)
	w.Write(ms)
}

// applyPropPatch walks req's set/remove instructions. A <set> of
// calendar-color with a parseable hex is staged (last one wins if repeated)
// and, if any was staged, persisted via a single CalendarService.Update
// call (preserving the calendar's current Name) before returning — so a
// malformed later instruction can't leave a partial write. It reports, for
// that property, 200 OK with the canonical hex of the nearest-matched enum
// color — never the client's raw input (ADR-0026's read-back guard, applied
// here to the Calendar-collection resource). A <set> with an unparseable
// value reports 409 Conflict and is not applied. A <remove> of
// calendar-color, and any <set>/<remove> of any other property name,
// reports 403 Forbidden and is not applied.
//
// req.Set is processed in full before req.Remove regardless of how the two
// were actually interleaved in the request body — a client naming
// calendar-color in both a <set> and a <remove> in one request (which
// doesn't correspond to any real client behavior) has the <remove> win
// rather than strict document order.
func (h *dispatchHandler) applyPropPatch(ctx context.Context, userID int64, calendarID string, existing repository.Calendar, req proppatchRequestBody) ([]proppatchPropResult, error) {
	var results []proppatchPropResult
	colorSeen := false
	var colorResult proppatchPropResult
	stagedColor := ""
	haveStagedColor := false

	rejectUnlessColor := func(prop proppatchPropElem) bool {
		if prop.XMLName.Local == calendarColorLocalName {
			return true
		}
		results = append(results, proppatchPropResult{Name: prop.XMLName, Status: proppatchStatusForbidden})
		return false
	}

	for _, set := range req.Set {
		for _, prop := range set.Prop.Props {
			if !rejectUnlessColor(prop) {
				continue
			}
			colorSeen = true
			color, ok := nearestColor(strings.TrimSpace(prop.Content))
			if !ok {
				colorResult = proppatchPropResult{Name: calendarColorPropName, Status: proppatchStatusConflict}
				haveStagedColor = false
				continue
			}
			stagedColor = color
			haveStagedColor = true
			colorResult = proppatchPropResult{Name: calendarColorPropName, Status: proppatchStatusOK}
		}
	}

	for _, remove := range req.Remove {
		for _, prop := range remove.Prop.Props {
			if !rejectUnlessColor(prop) {
				continue
			}
			colorSeen = true
			colorResult = proppatchPropResult{Name: calendarColorPropName, Status: proppatchStatusForbidden}
			haveStagedColor = false
		}
	}

	if colorSeen {
		results = append(results, colorResult)
	}

	if haveStagedColor {
		if _, err := h.backend.calendars.Update(ctx, userID, calendarID, existing.Name, stagedColor); err != nil {
			return nil, err
		}
	}

	return results, nil
}

// proppatchMultistatus is the RFC 4918 §14.16 <multistatus> response a
// PROPPATCH returns: exactly one <response> for the request URI (PROPPATCH
// never targets children), grouping property results by shared Status into
// one propstat each.
type proppatchMultistatus struct {
	XMLName  xml.Name          `xml:"DAV: multistatus"`
	Response proppatchResponse `xml:"DAV: response"`
}

type proppatchResponse struct {
	Href      string              `xml:"DAV: href"`
	Propstats []proppatchPropstat `xml:"DAV: propstat"`
}

type proppatchPropstat struct {
	Prop   proppatchProp `xml:"DAV: prop"`
	Status string        `xml:"DAV: status"`
}

// proppatchProp marshals as <prop> containing one empty child element per
// Names entry. Property names aren't known at compile time (they're
// whatever a client's propertyupdate body named), so this can't use static
// struct fields the way sync.go's fixed getetag/calendar-data props do;
// MarshalXML encodes each xml.Name directly instead.
type proppatchProp struct {
	Names []xml.Name
}

func (p proppatchProp) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	for _, name := range p.Names {
		if err := e.EncodeToken(xml.StartElement{Name: name}); err != nil {
			return err
		}
		if err := e.EncodeToken(xml.EndElement{Name: name}); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

// buildPropPatchMultistatus groups results by Status into one propstat per
// distinct status (in first-seen order) and wraps them in a single
// <response> for href.
func buildPropPatchMultistatus(href string, results []proppatchPropResult) []byte {
	var order []string
	byStatus := map[string][]xml.Name{}
	for _, res := range results {
		if _, seen := byStatus[res.Status]; !seen {
			order = append(order, res.Status)
		}
		byStatus[res.Status] = append(byStatus[res.Status], res.Name)
	}

	ms := proppatchMultistatus{Response: proppatchResponse{Href: href}}
	for _, status := range order {
		ms.Response.Propstats = append(ms.Response.Propstats, proppatchPropstat{
			Prop:   proppatchProp{Names: byStatus[status]},
			Status: status,
		})
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	if err := xml.NewEncoder(&buf).Encode(ms); err != nil {
		// Encoding a small, self-constructed struct of known-safe XML Names
		// cannot fail in practice; degrade to an empty multistatus rather
		// than propagate an error from a pure response-building function.
		return []byte(xml.Header + `<multistatus xmlns="DAV:"></multistatus>`)
	}
	return buf.Bytes()
}
