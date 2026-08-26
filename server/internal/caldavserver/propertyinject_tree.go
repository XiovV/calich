// propertyinject_tree.go is a parallel, structural reimplementation of
// propertyinject.go's regex-based property injection (#277): decode the
// recorded PROPFIND response into the xmlNode tree (xmlnode.go), patch the
// matching elements by xml.Name, and re-encode — instead of regex-matching
// <response>/<propstat>/<prop> in the serialized bytes and string-splicing
// replacements in.
//
// Nothing calls injectPropertyTree/injectPropertyTreeRaw yet: ctag.go and
// propfind.go still dispatch through propertyinject.go's injectProperty/
// injectPropertyRaw, and continue to be the mechanism actually serving
// getctag, calendar-color, current-user-privilege-set,
// managed-attachments-server-URL, max-attachment-size and
// max-attachments-per-resource. This file exists to prove the structural
// approach out and be exercised by its own tests; migrating the six patches
// over is deliberately a separate change.
package caldavserver

import (
	"context"
	"encoding/xml"
)

var (
	responseElementName = xml.Name{Space: davNamespace, Local: "response"}
	hrefElementName     = xml.Name{Space: davNamespace, Local: "href"}
	propstatElementName = xml.Name{Space: davNamespace, Local: "propstat"}
	propElementName     = xml.Name{Space: davNamespace, Local: "prop"}
	statusElementName   = xml.Name{Space: davNamespace, Local: "status"}
)

// injectPropertyTree is injectProperty's (propertyinject.go) tree-based
// counterpart: it rewrites every <response> in body whose href resolves via
// valueFor to replace its empty, 404 <localName/> with a 200 propstat in
// namespace xmlns carrying the real value, escaped as text. Blocks valueFor
// declines are returned unchanged, and body is returned unchanged (with err
// set) if it fails to parse as XML.
func injectPropertyTree(ctx context.Context, body []byte, localName, xmlns string, valueFor propertyValueFunc) ([]byte, error) {
	return injectPropertyTreeValue(ctx, body, localName, xmlns, valueFor, func(value string) ([]*xmlNode, error) {
		return []*xmlNode{newXMLTextNode(value)}, nil
	})
}

// injectPropertyTreeRaw is injectPropertyTree's counterpart for a property
// whose value is nested elements rather than text — current-user-privilege-set
// needs literal <privilege><read/></privilege> children. valueFor must
// return well-formed XML; it is parsed into its own nodes and spliced in as
// the target element's children. A valueFor that breaks that contract
// surfaces the same way a malformed body does — an error, not a crashed
// request handler — same as injectProperty's escape-hatch: bad-but-fatal is
// reported, not panicked.
func injectPropertyTreeRaw(ctx context.Context, body []byte, localName, xmlns string, valueFor propertyValueFunc) ([]byte, error) {
	return injectPropertyTreeValue(ctx, body, localName, xmlns, valueFor, func(value string) ([]*xmlNode, error) {
		return decodeXMLDocument([]byte(value))
	})
}

// injectPropertyTreeValue is injectPropertyTree and injectPropertyTreeRaw's
// shared core; render turns valueFor's result into the target element's
// children — a single escaped text node for injectPropertyTree, parsed
// verbatim XML nodes for injectPropertyTreeRaw.
func injectPropertyTreeValue(ctx context.Context, body []byte, localName, xmlns string, valueFor propertyValueFunc, render func(value string) ([]*xmlNode, error)) ([]byte, error) {
	nodes, err := decodeXMLDocument(body)
	if err != nil {
		return body, err
	}

	targetName := xml.Name{Space: xmlns, Local: localName}

	var responses []*xmlNode
	for _, n := range nodes {
		n.findAll(responseElementName, &responses)
	}

	for _, response := range responses {
		hrefNode := response.child(hrefElementName)
		if hrefNode == nil {
			continue
		}

		value, ok := valueFor(ctx, hrefNode.text())
		if !ok {
			continue
		}

		found := false
		var keptPropstats []*xmlNode
		for _, c := range response.children {
			name, ok := c.Name()
			if !ok || name != propstatElementName {
				keptPropstats = append(keptPropstats, c)
				continue
			}
			propstat := c

			prop := propstat.child(propElementName)
			if prop == nil {
				keptPropstats = append(keptPropstats, c)
				continue
			}

			// A malformed PROPFIND request can list the same property
			// more than once; go-webdav doesn't deduplicate (server.go
			// walks propfind.Prop.Raw verbatim), so every empty
			// targetName sibling must go, not just the first.
			var remaining []*xmlNode
			removedAny := false
			for _, p := range prop.children {
				if name, ok := p.Name(); ok && name == targetName && p.isEmptyElement() {
					removedAny = true
					continue
				}
				remaining = append(remaining, p)
			}
			if !removedAny {
				keptPropstats = append(keptPropstats, c)
				continue
			}
			found = true
			if len(remaining) == 0 {
				// localName was the only prop in this propstat — drop
				// it entirely rather than leave an empty <prop/>.
				continue
			}
			prop.children = remaining
			keptPropstats = append(keptPropstats, propstat)
		}
		if !found {
			continue
		}
		response.children = keptPropstats

		children, err := render(value)
		if err != nil {
			return body, err
		}
		newPropstat := newXMLElementNode(propstatElementName,
			newXMLElementNode(propElementName,
				newXMLElementNode(targetName, children...),
			),
			newXMLElementNode(statusElementName, newXMLTextNode("HTTP/1.1 200 OK")),
		)
		response.children = append(response.children, newPropstat)
	}

	return encodeXMLDocument(nodes)
}
