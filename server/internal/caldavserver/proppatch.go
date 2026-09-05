// proppatch.go implements PROPPATCH end-to-end outside go-webdav: the
// library's caldav.Backend interface has no PropPatch hook at all, and its
// private wrapper hardcodes PropPatch to always return 501 regardless of
// what any Backend supports (see handler.go's dispatch and backend.go's
// package doc). This backend recognizes two settable properties —
// Apple/DAVx⁵'s calendar-color (validated by service.NormalizeColor,
// ADR-0029) and DAV:displayname (the Calendar rename case) — staged
// together per RFC 4918 §9.2's atomicity rule (see applyPropPatch).
// DAV:displayname stays Owner-only (ADR-0034): rename is Calendar
// management, and no Role grants it. calendar-color is different since
// ADR-0038: the Owner's <set> still writes the Calendar's own colour (and
// is still refused while a Subscription governs it, ADR-0032), but any
// other principal with at least Viewer Access gets their own colour
// override written instead, and always succeeds — no client's colour
// picker ever lies (ADR-0028). Any other named property, and any <remove>
// of displayname or of the Owner's own calendar-color (both backing columns
// are NOT NULL — there is no "unset"), is reported 403 Forbidden per
// property rather than silently ignored. A non-owner's <remove> of
// calendar-color instead clears their override, since there's a real
// "unset" for a row that may simply not exist.
package caldavserver

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
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
	// h.backend.calendars.Get resolved existing via Access, which admits a
	// Viewer or Editor too (ADR-0034) — not just the Owner. Whether that's
	// enough to act on any given property, and whether a Subscription
	// (ADR-0032) blocks it, is now decided per-property inside
	// applyPropPatch: displayname and the Owner's own calendar-color stay
	// gated exactly as before, but a non-owner's calendar-color override
	// (ADR-0038) is neither Owner-only nor blocked by a Subscription.

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
// Conflict regardless of who's asking. Who the write actually lands on
// differs (ADR-0038): the Owner's stages against calendars.color and is
// still 403 while a Source governs it (ADR-0032, ADR-0052); any other
// principal's stages as their own colour override instead, unaffected by
// the Source clamp, since an override never touches the tracked column. A
// <set> of displayname is Owner-only regardless of a Source — a non-owner's
// is 403 Forbidden; the Owner's stages the trimmed value, an empty one
// stages 409 Conflict (the Name column is NOT NULL, the same constraint the
// REST API enforces as ErrInvalidName, surfaced as 400 there since
// PROPPATCH and the REST API report property/field errors differently),
// and a Calendar with any Source's is 403 Forbidden (renaming stays a
// web-app action). Both checks key off Source *existing*, not its Mode —
// deliberately unlike ResolveAccess's write clamp (#284, ADR-0052): a Source
// carrying a writable Mode still tracks the Provider's own name/colour via
// the shadow columns (ADR-0032, ADR-0052's "presentation is local"), and a
// raw CalDAV property write has no way to signal "I know this diverges from
// the feed" the way the web app's rename flow does — so the web-app-only
// rule stands regardless of whether the underlying Source can otherwise be
// written to. A <remove> of displayname, or of the Owner's own
// calendar-color, is 403 Forbidden (both backing columns are NOT NULL —
// there is no "unset"); a non-owner's <remove> of calendar-color instead
// clears their override and stages 200 OK. Any <set>/<remove> of any other
// property name stages 403 Forbidden.
func (h *dispatchHandler) applyPropPatch(ctx context.Context, userID int64, calendarID string, existing repository.Calendar, req proppatchRequestBody) ([]proppatchPropResult, error) {
	isOwner := existing.UserID == userID

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
				if isOwner && existing.Source != nil {
					stage(calendarColorPropName, proppatchStatusForbidden, false, "")
					continue
				}
				stage(calendarColorPropName, proppatchStatusOK, true, color)
			case displayNameLocalName:
				if !isOwner {
					stage(displayNamePropName, proppatchStatusForbidden, false, "")
					continue
				}
				name := strings.TrimSpace(prop.Content)
				if name == "" {
					stage(displayNamePropName, proppatchStatusConflict, false, "")
					continue
				}
				if existing.Source != nil {
					stage(displayNamePropName, proppatchStatusForbidden, false, "")
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
				if isOwner {
					stage(calendarColorPropName, proppatchStatusForbidden, false, "")
					continue
				}
				// A non-owner's colour override has a real "unset" — the
				// row may simply not exist — unlike the Owner's NOT NULL
				// column (ADR-0038).
				stage(calendarColorPropName, proppatchStatusOK, true, "")
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
	nameChanged, colorChanged := false, false
	overrideColor := ""
	overrideSet, overrideCleared := false, false
	results := make([]proppatchPropResult, 0, len(order))
	for _, name := range order {
		p := staged[name]
		status := p.status
		if anyFailed && p.ok {
			status = proppatchStatusFailedDependency
		}
		results = append(results, proppatchPropResult{Name: p.name, Status: status})

		if !anyFailed && p.ok {
			switch name {
			case calendarColorPropName:
				if isOwner {
					newColor = p.newValue
					colorChanged = true
				} else if p.newValue == "" {
					overrideCleared = true
				} else {
					overrideColor = p.newValue
					overrideSet = true
				}
			case displayNamePropName:
				newName = p.newValue
				nameChanged = true
			}
		}
	}

	if nameChanged || colorChanged {
		if _, err := h.backend.calendars.Update(ctx, userID, calendarID, service.CalendarWrite{Name: newName, Color: newColor}, false, false); err != nil {
			return nil, err
		}
	}
	if overrideSet {
		if _, err := h.backend.calendars.SetColorOverride(ctx, userID, calendarID, overrideColor); err != nil {
			return nil, err
		}
	}
	if overrideCleared {
		if err := h.backend.calendars.ClearColorOverride(ctx, userID, calendarID); err != nil {
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
