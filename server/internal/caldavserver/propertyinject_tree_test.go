package caldavserver

import (
	"context"
	"encoding/xml"
	"strings"
	"testing"
)

// findPropstats returns every direct <propstat> child of response, decoded
// from injectPropertyTree(Raw)'s output.
func findPropstats(t *testing.T, response *xmlNode) []*xmlNode {
	t.Helper()
	var out []*xmlNode
	for _, c := range response.children {
		if name, ok := c.Name(); ok && name == propstatElementName {
			out = append(out, c)
		}
	}
	return out
}

func propstatStatusText(t *testing.T, propstat *xmlNode) string {
	t.Helper()
	status := propstat.child(statusElementName)
	if status == nil {
		t.Fatalf("propstat has no <status>")
	}
	return status.text()
}

func decodeSingleResponse(t *testing.T, body []byte) *xmlNode {
	t.Helper()
	nodes, err := decodeXMLDocument(body)
	if err != nil {
		t.Fatalf("decodeXMLDocument: %v", err)
	}
	var responses []*xmlNode
	for _, n := range nodes {
		n.findAll(responseElementName, &responses)
	}
	if len(responses) != 1 {
		t.Fatalf("got %d <response> nodes, want 1:\n%s", len(responses), body)
	}
	return responses[0]
}

// twoPropstatBody builds a <response> for href whose 200 propstat carries
// <resourcetype><collection/></resourcetype> and whose 404 propstat carries
// unknownProps, each an empty element in xmlns — the exact shape
// go-webdav's NewRawXMLElement(name, nil, nil) (server.go) produces for a
// requested-but-unsupported property. The two propstats sit back-to-back
// with no whitespace between "</propstat>" and the next "<propstat>", the
// adversarial case that could confuse a naive substring/prefix scan of
// "<prop" against "<propstat" (propertyinject.go's propRe comment).
func twoPropstatBody(href, xmlns string, unknownProps ...string) []byte {
	var unknown strings.Builder
	for _, p := range unknownProps {
		unknown.WriteString(`<` + p + ` xmlns="` + xmlns + `"></` + p + `>`)
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<multistatus xmlns="DAV:"><response><href>` + href + `</href>` +
		`<propstat><prop><resourcetype><collection/></resourcetype></prop><status>HTTP/1.1 200 OK</status></propstat>` +
		`<propstat><prop>` + unknown.String() + `</prop><status>HTTP/1.1 404 Not Found</status></propstat>` +
		`</response></multistatus>`)
}

func TestInjectPropertyTree_ReplacesEmptyPropertyWith200Propstat_KeepsSiblingUnknownProp(t *testing.T) {
	body := twoPropstatBody("/cal/a/", getctagNamespace, "getctag", "other-unknown")

	out, err := injectPropertyTree(context.Background(), body, "getctag", getctagNamespace, func(ctx context.Context, href string) (string, bool) {
		if href != "/cal/a/" {
			t.Fatalf("unexpected href %q", href)
		}
		return "42", true
	})
	if err != nil {
		t.Fatalf("injectPropertyTree: %v", err)
	}

	response := decodeSingleResponse(t, out)
	propstats := findPropstats(t, response)
	if len(propstats) != 3 {
		t.Fatalf("got %d propstats, want 3 (200 resourcetype, 404 other-unknown, 200 getctag):\n%s", len(propstats), out)
	}

	var got200getctag, got404 bool
	getctagName := xml.Name{Space: getctagNamespace, Local: "getctag"}
	otherName := xml.Name{Space: getctagNamespace, Local: "other-unknown"}
	for _, ps := range propstats {
		prop := ps.child(propElementName)
		if prop == nil {
			t.Fatalf("propstat missing <prop>: %+v", ps)
		}
		if gc := prop.child(getctagName); gc != nil {
			if gc.isEmptyElement() {
				t.Fatalf("getctag still empty in a surviving propstat")
			}
			if got := gc.text(); got != "42" {
				t.Fatalf("getctag text = %q, want 42", got)
			}
			if status := propstatStatusText(t, ps); status != "HTTP/1.1 200 OK" {
				t.Fatalf("getctag propstat status = %q, want 200", status)
			}
			got200getctag = true
		}
		if oth := prop.child(otherName); oth != nil {
			if !oth.isEmptyElement() {
				t.Fatalf("other-unknown should be untouched (still empty)")
			}
			if status := propstatStatusText(t, ps); status != "HTTP/1.1 404 Not Found" {
				t.Fatalf("other-unknown propstat status = %q, want 404", status)
			}
			got404 = true
		}
	}
	if !got200getctag || !got404 {
		t.Fatalf("missing expected propstats: got200getctag=%v got404=%v", got200getctag, got404)
	}
}

func TestInjectPropertyTree_DropsPropstatWhenItsOnlyPropertyIsPatched(t *testing.T) {
	body := twoPropstatBody("/cal/a/", getctagNamespace, "getctag")

	out, err := injectPropertyTree(context.Background(), body, "getctag", getctagNamespace, func(ctx context.Context, href string) (string, bool) {
		return "7", true
	})
	if err != nil {
		t.Fatalf("injectPropertyTree: %v", err)
	}

	response := decodeSingleResponse(t, out)
	propstats := findPropstats(t, response)
	if len(propstats) != 2 {
		t.Fatalf("got %d propstats, want 2 (200 resourcetype, 200 getctag) — the emptied 404 propstat should be dropped:\n%s", len(propstats), out)
	}
	for _, ps := range propstats {
		if status := propstatStatusText(t, ps); status == "HTTP/1.1 404 Not Found" {
			t.Fatalf("a 404 propstat survived: %+v", ps)
		}
	}
}

func TestInjectPropertyTree_ValueForDeclines_LeavesResponseUnchanged(t *testing.T) {
	body := twoPropstatBody("/cal/a/", getctagNamespace, "getctag")

	out, err := injectPropertyTree(context.Background(), body, "getctag", getctagNamespace, func(ctx context.Context, href string) (string, bool) {
		return "", false
	})
	if err != nil {
		t.Fatalf("injectPropertyTree: %v", err)
	}

	response := decodeSingleResponse(t, out)
	propstats := findPropstats(t, response)
	if len(propstats) != 2 {
		t.Fatalf("got %d propstats, want 2 (unchanged)", len(propstats))
	}
	getctagName := xml.Name{Space: getctagNamespace, Local: "getctag"}
	found := false
	for _, ps := range propstats {
		prop := ps.child(propElementName)
		if prop == nil {
			continue
		}
		if gc := prop.child(getctagName); gc != nil {
			if !gc.isEmptyElement() {
				t.Fatalf("getctag should still be empty/unpatched")
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the original empty getctag to survive untouched")
	}
}

func TestInjectPropertyTree_SelfClosedAndOpenCloseEmptyElement_PatchIdentically(t *testing.T) {
	valueFor := func(ctx context.Context, href string) (string, bool) { return "9", true }

	openClose := []byte(`<multistatus xmlns="DAV:"><response><href>/cal/a/</href>` +
		`<propstat><prop><getctag xmlns="` + getctagNamespace + `"></getctag></prop><status>HTTP/1.1 404 Not Found</status></propstat>` +
		`</response></multistatus>`)
	selfClosed := []byte(`<multistatus xmlns="DAV:"><response><href>/cal/a/</href>` +
		`<propstat><prop><getctag xmlns="` + getctagNamespace + `"/></prop><status>HTTP/1.1 404 Not Found</status></propstat>` +
		`</response></multistatus>`)

	getctagName := xml.Name{Space: getctagNamespace, Local: "getctag"}
	for name, body := range map[string][]byte{"open-close": openClose, "self-closed": selfClosed} {
		out, err := injectPropertyTree(context.Background(), body, "getctag", getctagNamespace, valueFor)
		if err != nil {
			t.Fatalf("[%s] injectPropertyTree: %v", name, err)
		}
		response := decodeSingleResponse(t, out)
		propstats := findPropstats(t, response)
		if len(propstats) != 1 {
			t.Fatalf("[%s] got %d propstats, want 1", name, len(propstats))
		}
		gc := propstats[0].child(propElementName).child(getctagName)
		if gc == nil || gc.isEmptyElement() || gc.text() != "9" {
			t.Fatalf("[%s] getctag = %+v, want text \"9\"", name, gc)
		}
	}
}

func TestInjectPropertyTreeRaw_SplicesParsedChildNodesVerbatim(t *testing.T) {
	body := twoPropstatBody("/cal/a/", davNamespace, "current-user-privilege-set")

	out, err := injectPropertyTreeRaw(context.Background(), body, "current-user-privilege-set", davNamespace, func(ctx context.Context, href string) (string, bool) {
		return "<privilege><read/></privilege>", true
	})
	if err != nil {
		t.Fatalf("injectPropertyTreeRaw: %v", err)
	}

	response := decodeSingleResponse(t, out)
	targetName := xml.Name{Space: davNamespace, Local: "current-user-privilege-set"}
	var target *xmlNode
	for _, ps := range findPropstats(t, response) {
		if prop := ps.child(propElementName); prop != nil {
			if n := prop.child(targetName); n != nil {
				target = n
			}
		}
	}
	if target == nil {
		t.Fatalf("current-user-privilege-set not found in output:\n%s", out)
	}

	// valueFor's fragment declares no xmlns of its own, so once spliced
	// under <current-user-privilege-set xmlns="DAV:">, "privilege" and
	// "read" resolve to the DAV: namespace by ordinary XML inheritance —
	// re-decoding the final bytes here confirms a real client would see
	// exactly the RFC 3744 DAV:privilege/DAV:read the regex mechanism's
	// literal splice also relies on inheritance to produce.
	privilegeName := xml.Name{Space: davNamespace, Local: "privilege"}
	readName := xml.Name{Space: davNamespace, Local: "read"}

	privilege := target.child(privilegeName)
	if privilege == nil {
		t.Fatalf("expected a <privilege> child, got children: %+v", target.children)
	}
	if privilege.child(readName) == nil {
		t.Fatalf("expected <privilege> to contain <read/>")
	}
}

func TestInjectPropertyTreeRaw_ReturnsErrorOnMalformedValue(t *testing.T) {
	body := twoPropstatBody("/cal/a/", davNamespace, "current-user-privilege-set")

	_, err := injectPropertyTreeRaw(context.Background(), body, "current-user-privilege-set", davNamespace, func(ctx context.Context, href string) (string, bool) {
		return "<privilege><read/>", true // unclosed <privilege>
	})
	if err == nil {
		t.Fatalf("expected an error when valueFor returns malformed XML, got nil")
	}
}

// TestInjectPropertyTree_RemovesEveryDuplicateEmptyProperty covers a client
// PROPFIND requesting the same property twice in one <prop> block — nothing
// in go-webdav deduplicates that (server.go's NewPropFindResponse walks
// propfind.Prop.Raw verbatim), so the 404 propstat go-webdav emits can carry
// two empty <getctag/> siblings. Both must be removed, or the response would
// still carry a stray empty (and thus, per WebDAV, ambiguous/incomplete)
// <getctag/> alongside the new 200 propstat.
func TestInjectPropertyTree_RemovesEveryDuplicateEmptyProperty(t *testing.T) {
	body := []byte(`<multistatus xmlns="DAV:"><response><href>/cal/a/</href>` +
		`<propstat><prop>` +
		`<getctag xmlns="` + getctagNamespace + `"></getctag>` +
		`<getctag xmlns="` + getctagNamespace + `"></getctag>` +
		`</prop><status>HTTP/1.1 404 Not Found</status></propstat>` +
		`</response></multistatus>`)

	out, err := injectPropertyTree(context.Background(), body, "getctag", getctagNamespace, func(ctx context.Context, href string) (string, bool) {
		return "5", true
	})
	if err != nil {
		t.Fatalf("injectPropertyTree: %v", err)
	}

	response := decodeSingleResponse(t, out)
	propstats := findPropstats(t, response)
	if len(propstats) != 1 {
		t.Fatalf("got %d propstats, want 1 (the emptied 404 propstat should be dropped, leaving only the new 200 one):\n%s", len(propstats), out)
	}
	getctagName := xml.Name{Space: getctagNamespace, Local: "getctag"}
	gc := propstats[0].child(propElementName).child(getctagName)
	if gc == nil || gc.isEmptyElement() || gc.text() != "5" {
		t.Fatalf("getctag = %+v, want text \"5\"", gc)
	}
}
