// proppatch.go implements PROPPATCH end-to-end outside go-webdav: the
// library's caldav.Backend interface has no PropPatch hook at all, and its
// private wrapper hardcodes PropPatch to always return 501 regardless of
// what any Backend supports (see handler.go's dispatch and backend.go's
// package doc). This backend recognizes two settable properties —
// Apple/DAVx⁵'s calendar-color (validated by service.NormalizeColor,
// ADR-0029) and DAV:displayname (the Calendar rename case) — staged
// together per RFC 4918 §9.2's atomicity rule (see applyPropPatch). Any
// other named property, and any <remove> of either settable property (both
// backing columns are NOT NULL — there is no "unset"), is reported 403
// Forbidden per property rather than silently ignored, so a client can tell
// its update didn't fully apply.
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
	"github.com/XiovV/calendar/server/internal/service"
)

const (
	proppatchStatusOK               = "HTTP/1.1 200 OK"
	proppatchStatusForbidden        = "HTTP/1.1 403 Forbidden"
	proppatchStatusConflict         = "HTTP/1.1 409 Conflict"
	proppatchStatusFailedDependency = "HTTP/1.1 424 Failed Dependency"
	calendarColorLocalName          = "calendar-color"
	displayNameLocalName            = "displayname"
	davNamespace                    = "DAV:"
)

var (
	calendarColorPropName = xml.Name{Space: calendarColorNamespace, Local: calendarColorLocalName}
	displayNamePropName   = xml.Name{Space: davNamespace, Local: displayNameLocalName}
)

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
	// A Subscribed Calendar's collection is advertised as read-only via its
	// current-user-privilege-set (propfind.go's applyPrivilegeSetPatch,
	// ADR-0032) — renaming or recoloring one over CalDAV would be incoherent
	// with that, so PROPPATCH is refused outright rather than staged
	// per-property. Renaming and recoloring stay web-app actions.
	if existing.SourceURL != nil {
		http.Error(w, "calendar is subscribed and read-only", http.StatusForbidden)
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

// stagedProp is one named property's outcome while applyPropPatch walks the
// request. ok is true only for a <set> instruction whose value validated —
// provisionally 200, carrying the value that would be written. Everything
// else (an unparseable <set>, any <remove>, any unrecognized property name)
// is already a final, non-2xx outcome and never becomes ok.
type stagedProp struct {
	name     xml.Name
	status   string
	ok       bool
	newValue string
}

// applyPropPatch walks req's set/remove instructions and stages one outcome
// per named property (last instruction for a given name wins, so req.Set is
// processed in full before req.Remove regardless of how the two were
// actually interleaved in the request body — a client naming the same
// property in both a <set> and a <remove> in one request, which doesn't
// correspond to any real client behavior, has the <remove> win rather than
// strict document order).
//
// Per RFC 4918 §9.2, PROPPATCH is atomic across the whole request: if any
// staged outcome is not 2xx, nothing is written, and every would-be-200 is
// downgraded to 424 Failed Dependency instead. This is what lets a client
// rename a Calendar and set its color in one request and have a malformed
// color leave the rename unapplied too, rather than silently renaming a
// Calendar whose color update the client believes failed on its own.
//
// A <set> of calendar-color with a parseable hex stages
// service.NormalizeColor's canonical form — uppercase, alpha widened to FF
// for a 3- or 6-digit input — so an 8-digit client value still round-trips
// byte-identical up to case (ADR-0029); an unparseable value stages 409
// Conflict. A <set> of displayname with a non-empty value stages that
// value; an empty value
// stages 409 Conflict — the Name column is NOT NULL, the same constraint
// the REST API enforces as ErrInvalidName (there, surfaced as 400, since
// PROPPATCH and the REST API report property/field errors differently). A
// <remove> of either, and any <set>/<remove> of any other property name,
// stages 403 Forbidden.
func (h *dispatchHandler) applyPropPatch(ctx context.Context, userID int64, calendarID string, existing repository.Calendar, req proppatchRequestBody) ([]proppatchPropResult, error) {
	var order []xml.Name
	staged := map[xml.Name]stagedProp{}

	stage := func(name xml.Name, status string, ok bool, newValue string) {
		if _, seen := staged[name]; !seen {
			order = append(order, name)
		}
		staged[name] = stagedProp{name: name, status: status, ok: ok, newValue: newValue}
	}

	for _, set := range req.Set {
		for _, prop := range set.Prop.Props {
			switch prop.XMLName.Local {
			case calendarColorLocalName:
				color, ok := service.NormalizeColor(strings.TrimSpace(prop.Content))
				if !ok {
					stage(calendarColorPropName, proppatchStatusConflict, false, "")
					continue
				}
				stage(calendarColorPropName, proppatchStatusOK, true, color)
			case displayNameLocalName:
				name := strings.TrimSpace(prop.Content)
				if name == "" {
					stage(displayNamePropName, proppatchStatusConflict, false, "")
					continue
				}
				stage(displayNamePropName, proppatchStatusOK, true, name)
			default:
				stage(prop.XMLName, proppatchStatusForbidden, false, "")
			}
		}
	}

	for _, remove := range req.Remove {
		for _, prop := range remove.Prop.Props {
			switch prop.XMLName.Local {
			case calendarColorLocalName:
				stage(calendarColorPropName, proppatchStatusForbidden, false, "")
			case displayNameLocalName:
				stage(displayNamePropName, proppatchStatusForbidden, false, "")
			default:
				stage(prop.XMLName, proppatchStatusForbidden, false, "")
			}
		}
	}

	anyFailed := false
	for _, name := range order {
		if staged[name].status != proppatchStatusOK {
			anyFailed = true
			break
		}
	}

	newName, newColor := existing.Name, existing.Color
	changed := false
	results := make([]proppatchPropResult, 0, len(order))
	for _, name := range order {
		p := staged[name]
		status := p.status
		if anyFailed && p.ok {
			status = proppatchStatusFailedDependency
		}
		results = append(results, proppatchPropResult{Name: p.name, Status: status})

		if !anyFailed && p.ok {
			changed = true
			switch name {
			case calendarColorPropName:
				newColor = p.newValue
			case displayNamePropName:
				newName = p.newValue
			}
		}
	}

	if changed {
		if _, err := h.backend.calendars.Update(ctx, userID, calendarID, service.CalendarWrite{Name: newName, Color: newColor}); err != nil {
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
