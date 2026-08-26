// xmlnode.go underlies propertyinject_tree.go's structural property
// injection (#277): a generic, ordered XML node tree that decodes any XML
// document via encoding/xml's token stream, exposes its elements for
// matching by xml.Name rather than by tag-name substring, and re-encodes
// losslessly. Decoding through the standard tokenizer means self-closed
// (<foo/>) and open/close (<foo></foo>) empty elements are already
// indistinguishable by the time they reach a node, and matching by xml.Name
// (namespace + local name, not a tag-name string) makes a "prop" tag ever
// matching "propstat" structurally impossible.
package caldavserver

import (
	"bytes"
	"encoding/xml"
	"io"
)

// xmlNode is one node of a decoded document: an element (token is an
// xml.StartElement; children holds its nested nodes in document order) or a
// leaf token — xml.CharData, xml.Comment, xml.ProcInst, xml.Directive — with
// no children of its own.
type xmlNode struct {
	token    xml.Token
	children []*xmlNode
}

// newXMLTextNode wraps s as the escaped-on-encode text content of an
// element — the tree equivalent of propertyinject.go's xml.EscapeText call.
func newXMLTextNode(s string) *xmlNode {
	return &xmlNode{token: xml.CharData(s)}
}

// newXMLElementNode builds an element node with the given children, for
// assembling the replacement <propstat>/<prop>/<status> structure a patch
// splices in.
func newXMLElementNode(name xml.Name, children ...*xmlNode) *xmlNode {
	return &xmlNode{token: xml.StartElement{Name: name}, children: children}
}

// Name returns n's element name and true, or the zero Name and false if n
// isn't an element (character data, a comment, ...).
func (n *xmlNode) Name() (xml.Name, bool) {
	start, ok := n.token.(xml.StartElement)
	if !ok {
		return xml.Name{}, false
	}
	return start.Name, true
}

// isEmptyElement reports whether n is an element with no children — the
// shape go-webdav's NewRawXMLElement(name, nil, nil) (server.go) emits for a
// requested-but-unsupported property inside its 404 propstat, and so the
// shape a patch looks for to replace.
func (n *xmlNode) isEmptyElement() bool {
	_, ok := n.Name()
	return ok && len(n.children) == 0
}

// text concatenates n's direct xml.CharData children. The properties this
// package patches (href, getctag, calendar-color, ...) only ever carry plain
// text content, so direct children are all a caller needs.
func (n *xmlNode) text() string {
	var buf bytes.Buffer
	for _, c := range n.children {
		if cd, ok := c.token.(xml.CharData); ok {
			buf.Write(cd)
		}
	}
	return buf.String()
}

// child returns n's first direct child element named name, or nil.
func (n *xmlNode) child(name xml.Name) *xmlNode {
	for _, c := range n.children {
		if cn, ok := c.Name(); ok && cn == name {
			return c
		}
	}
	return nil
}

// findAll appends every descendant of n (n itself excluded), depth-first
// pre-order, whose element name equals name to out — used to locate every
// <response> in a decoded multistatus regardless of how deeply the caller's
// document nests them.
func (n *xmlNode) findAll(name xml.Name, out *[]*xmlNode) {
	for _, c := range n.children {
		if cn, ok := c.Name(); ok && cn == name {
			*out = append(*out, c)
		}
		c.findAll(name, out)
	}
}

// decodeXMLDocument decodes an entire XML byte stream — a full document
// (XML declaration plus root element) or a bare fragment (a raw-XML value
// like "<privilege><read/></privilege>") alike — into its top-level nodes in
// document order.
func decodeXMLDocument(body []byte) ([]*xmlNode, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))

	var nodes []*xmlNode
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nodes, nil
		}
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			child, err := decodeXMLNode(dec, t.Copy())
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, child)
		default:
			nodes = append(nodes, &xmlNode{token: xml.CopyToken(tok)})
		}
	}
}

// stripNamespaceDecls drops start's own xmlns/xmlns:prefix attributes.
// xml.Decoder.Token resolves them into StartElement.Name.Space (and every
// descendant's) but also leaves them sitting in Attr — if kept there,
// re-encoding would emit them a second time, since xml.Encoder.EncodeToken
// already writes its own "xmlns" attribute for any element whose Name.Space
// is non-empty (encoding/xml's writeStart), regardless of what Attr holds.
func stripNamespaceDecls(start xml.StartElement) xml.StartElement {
	var kept []xml.Attr
	for _, a := range start.Attr {
		if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
			continue
		}
		kept = append(kept, a)
	}
	start.Attr = kept
	return start
}

// decodeXMLNode decodes start's subtree, consuming tokens up to and
// including its matching xml.EndElement. start must already be owned by the
// caller (via StartElement.Copy) — the decoder reuses the backing storage of
// tokens it returns for the next Token() call, so anything kept around, an
// element's Name/Attr or a CharData's bytes, must be copied out first
// (mirrors the pattern go-webdav's own internal.RawXMLValue uses for the
// same reason).
func decodeXMLNode(dec *xml.Decoder, start xml.StartElement) (*xmlNode, error) {
	node := &xmlNode{token: stripNamespaceDecls(start)}

	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			child, err := decodeXMLNode(dec, t.Copy())
			if err != nil {
				return nil, err
			}
			node.children = append(node.children, child)
		case xml.EndElement:
			return node, nil
		default:
			node.children = append(node.children, &xmlNode{token: xml.CopyToken(tok)})
		}
	}
}

// encodeXMLDocument re-encodes nodes (as returned by decodeXMLDocument) back
// into an XML byte stream.
func encodeXMLDocument(nodes []*xmlNode) ([]byte, error) {
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	for _, n := range nodes {
		if err := encodeXMLNode(enc, n); err != nil {
			return nil, err
		}
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeXMLNode(enc *xml.Encoder, n *xmlNode) error {
	start, ok := n.token.(xml.StartElement)
	if !ok {
		return enc.EncodeToken(n.token)
	}

	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	for _, child := range n.children {
		if err := encodeXMLNode(enc, child); err != nil {
			return err
		}
	}
	return enc.EncodeToken(start.End())
}
