// propertyinject.go generalizes the XML-patching technique ctag.go
// introduced for getctag (ADR-0025, #65) so any PROPFIND property go-webdav
// has no hook to inject — getctag, calendar-color (ADR-0028), and any future
// one — can reuse the same "record via httptest.Recorder, then regex-patch
// the empty/404 propstat into a real 200 one" mechanism instead of
// duplicating it.
package caldavserver

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"regexp"
)

var (
	responseRe = regexp.MustCompile(`(?s)<response[^>]*>.*?</response>`)
	hrefRe     = regexp.MustCompile(`(?s)<href[^>]*>(.*?)</href>`)
	propstatRe = regexp.MustCompile(`(?s)<propstat[^>]*>.*?</propstat>`)
	// "prop" is a prefix of "propstat", so the tag match requires a space or
	// ">" right after it — otherwise this accidentally matches <propstat ...>
	// itself and every propstat looks "empty".
	propRe = regexp.MustCompile(`(?s)<prop(?:\s[^>]*)?>(.*?)</prop>`)
)

// propertyValueFunc resolves the value href's response should carry for one
// property, or ok=false to leave that block's existing propstat alone (not
// a Calendar collection, or nothing to report).
type propertyValueFunc func(ctx context.Context, href string) (value string, ok bool)

// emptyElementRe matches both the open/close form Go's encoding/xml emits
// for an empty element (<localName ...></localName>) and a defensive
// self-closed form (<localName .../>), so this keeps working even if that
// emission detail ever changes.
func emptyElementRe(localName string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)<` + localName + `[^>]*/>|<` + localName + `[^>]*>.*?</` + localName + `>`)
}

// injectProperty rewrites every <response> block in body whose href
// resolves via valueFor to replace its empty, 404 <localName/> with a 200
// propstat in namespace xmlns carrying the real value. Blocks valueFor
// declines (not a Calendar collection, or localName not present) are
// returned unchanged.
func injectProperty(ctx context.Context, body []byte, localName, xmlns string, valueFor propertyValueFunc) []byte {
	elementRe := emptyElementRe(localName)

	return responseRe.ReplaceAllFunc(body, func(block []byte) []byte {
		hrefMatch := hrefRe.FindSubmatch(block)
		if hrefMatch == nil {
			return block
		}

		value, ok := valueFor(ctx, string(hrefMatch[1]))
		if !ok {
			return block
		}

		found := false
		block = propstatRe.ReplaceAllFunc(block, func(propstat []byte) []byte {
			if !elementRe.Match(propstat) {
				return propstat
			}
			found = true

			stripped := elementRe.ReplaceAll(propstat, nil)
			if propMatch := propRe.FindSubmatch(stripped); propMatch != nil && len(bytes.TrimSpace(propMatch[1])) == 0 {
				// localName was the only prop in this propstat — drop it
				// entirely rather than leave an empty <prop/>.
				return nil
			}
			return stripped
		})
		if !found {
			return block
		}

		var buf bytes.Buffer
		xml.EscapeText(&buf, []byte(value))
		newPropstat := fmt.Sprintf(
			`<propstat xmlns="DAV:"><prop xmlns="DAV:"><%s xmlns=%q>%s</%s></prop><status xmlns="DAV:">HTTP/1.1 200 OK</status></propstat>`,
			localName, xmlns, buf.String(), localName,
		)
		return bytes.Replace(block, []byte("</response>"), []byte(newPropstat+"</response>"), 1)
	})
}
