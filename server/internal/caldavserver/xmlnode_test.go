package caldavserver

import (
	"encoding/xml"
	"strings"
	"testing"
)

func mustDecodeXMLDocument(t *testing.T, body string) []*xmlNode {
	t.Helper()
	nodes, err := decodeXMLDocument([]byte(body))
	if err != nil {
		t.Fatalf("decodeXMLDocument(%q): %v", body, err)
	}
	return nodes
}

// rootElement returns the single element among nodes' top-level entries,
// skipping any leading xml.ProcInst/xml.CharData (the "<?xml ...?>" header
// and any surrounding whitespace).
func rootElement(t *testing.T, nodes []*xmlNode) *xmlNode {
	t.Helper()
	for _, n := range nodes {
		if _, ok := n.Name(); ok {
			return n
		}
	}
	t.Fatalf("no root element among %d top-level nodes", len(nodes))
	return nil
}

func TestXMLNode_DecodeEncode_RoundTripsStructure(t *testing.T) {
	const body = `<?xml version="1.0" encoding="UTF-8"?>` +
		`<multistatus xmlns="DAV:"><response><href>/cal/</href>` +
		`<propstat><prop><displayname>Home</displayname></prop>` +
		`<status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`

	nodes := mustDecodeXMLDocument(t, body)
	root := rootElement(t, nodes)

	name, ok := root.Name()
	if !ok || name != (xml.Name{Space: davNamespace, Local: "multistatus"}) {
		t.Fatalf("root name = %+v, ok=%v", name, ok)
	}

	responses := root.child(responseElementName)
	if responses == nil {
		t.Fatalf("no <response> child found")
	}
	href := responses.child(hrefElementName)
	if href == nil || href.text() != "/cal/" {
		t.Fatalf("href = %+v, want /cal/", href)
	}

	encoded, err := encodeXMLDocument(nodes)
	if err != nil {
		t.Fatalf("encodeXMLDocument: %v", err)
	}

	// Re-decode the re-encoded bytes and check the same structure survives
	// — byte-for-byte equality isn't expected (the encoder re-declares
	// xmlns on every namespaced element instead of relying on inheritance
	// like the original compact body did), but the content must not have
	// been lost or reshuffled.
	again := mustDecodeXMLDocument(t, string(encoded))
	root2 := rootElement(t, again)
	href2 := root2.child(responseElementName).child(hrefElementName)
	if href2 == nil || href2.text() != "/cal/" {
		t.Fatalf("after round-trip, href = %+v, want /cal/\nencoded: %s", href2, encoded)
	}
}

func TestXMLNode_SelfClosedAndOpenClose_DecodeIdentically(t *testing.T) {
	selfClosed := mustDecodeXMLDocument(t, `<prop><getctag xmlns="http://calendarserver.org/ns/"/></prop>`)
	openClose := mustDecodeXMLDocument(t, `<prop><getctag xmlns="http://calendarserver.org/ns/"></getctag></prop>`)

	a := rootElement(t, selfClosed)
	b := rootElement(t, openClose)

	getctagName := xml.Name{Space: getctagNamespace, Local: "getctag"}
	aChild := a.child(getctagName)
	bChild := b.child(getctagName)

	if aChild == nil || bChild == nil {
		t.Fatalf("getctag child missing: self-closed=%v open-close=%v", aChild, bChild)
	}
	if !aChild.isEmptyElement() || !bChild.isEmptyElement() {
		t.Fatalf("expected both forms to decode as empty elements: self-closed=%d children, open-close=%d children",
			len(aChild.children), len(bChild.children))
	}
}

func TestXMLNode_FindAll_LocatesNestedDescendantsByName(t *testing.T) {
	const body = `<multistatus xmlns="DAV:">` +
		`<response><href>/a/</href></response>` +
		`<response><href>/b/</href></response>` +
		`</multistatus>`

	nodes := mustDecodeXMLDocument(t, body)
	var responses []*xmlNode
	for _, n := range nodes {
		n.findAll(responseElementName, &responses)
	}

	if len(responses) != 2 {
		t.Fatalf("found %d <response> nodes, want 2", len(responses))
	}
	if got := responses[0].child(hrefElementName).text(); got != "/a/" {
		t.Fatalf("responses[0] href = %q, want /a/", got)
	}
	if got := responses[1].child(hrefElementName).text(); got != "/b/" {
		t.Fatalf("responses[1] href = %q, want /b/", got)
	}
}

func TestXMLNode_IsEmptyElement_FalseForTextOrElementContent(t *testing.T) {
	nodes := mustDecodeXMLDocument(t, `<a><withText>hi</withText><withChild><b/></withChild><empty/></a>`)
	root := rootElement(t, nodes)

	cases := []struct {
		local string
		want  bool
	}{
		{"withText", false},
		{"withChild", false},
		{"empty", true},
	}
	for _, c := range cases {
		node := root.child(xml.Name{Local: c.local})
		if node == nil {
			t.Fatalf("missing <%s>", c.local)
		}
		if got := node.isEmptyElement(); got != c.want {
			t.Errorf("<%s>.isEmptyElement() = %v, want %v", c.local, got, c.want)
		}
	}
}

func TestXMLNode_Encode_DoesNotDuplicateInheritedNamespaceDeclaration(t *testing.T) {
	// The source xmlns="DAV:" declaration on <multistatus> is both resolved
	// into every descendant's Name.Space by the decoder AND left sitting in
	// <multistatus>'s own Attr slice — if a node's Attr kept it, re-encoding
	// would emit it a second time (encoding/xml's encoder already writes
	// its own xmlns for any element with a non-empty Name.Space).
	nodes := mustDecodeXMLDocument(t, `<multistatus xmlns="DAV:"><response/></multistatus>`)
	encoded, err := encodeXMLDocument(nodes)
	if err != nil {
		t.Fatalf("encodeXMLDocument: %v", err)
	}

	// Every DAV: element re-declares its own xmlns (the encoder has no
	// inheritance tracking), so the whole document legitimately contains
	// two xmlns="DAV:" occurrences — one on <multistatus>, one on
	// <response>. What must not happen is either element's own opening
	// tag carrying it twice.
	openTag := strings.SplitN(string(encoded), ">", 2)[0]
	if n := strings.Count(openTag, `xmlns="DAV:"`); n != 1 {
		t.Fatalf(`expected exactly one xmlns="DAV:" on <multistatus>'s own tag, got %d: %s`, n, openTag)
	}
}

func TestXMLNode_Encode_EscapesTextContent(t *testing.T) {
	nodes := []*xmlNode{
		newXMLElementNode(xml.Name{Local: "a"}, newXMLTextNode("<hi> & \"bye\"")),
	}
	encoded, err := encodeXMLDocument(nodes)
	if err != nil {
		t.Fatalf("encodeXMLDocument: %v", err)
	}
	if strings.Contains(string(encoded), "<hi>") {
		t.Fatalf("expected text content to be escaped, got: %s", encoded)
	}

	// And it must decode back to the original text.
	again := mustDecodeXMLDocument(t, string(encoded))
	if got := rootElement(t, again).text(); got != `<hi> & "bye"` {
		t.Fatalf("round-tripped text = %q, want `<hi> & \"bye\"`", got)
	}
}
