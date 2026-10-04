// Package htmldoc handles task documents written as a self-contained HTML
// page: sanitizing them on save and reading them back as plain text for agent
// context.
package htmldoc

import (
	"strings"

	"golang.org/x/net/html"
)

// droppedElements are removed with everything inside them. The report is
// rendered in the desktop app next to the reviewer's controls, so anything
// that runs code, loads another document, posts a form or re-targets relative
// URLs has no place in it. SVG animation elements are here because
// <set attributeName="href" to="javascript:…"> rewrites a link after the
// attribute filter has already looked at it.
var droppedElements = map[string]bool{
	"script":           true,
	"iframe":           true,
	"frame":            true,
	"frameset":         true,
	"object":           true,
	"embed":            true,
	"applet":           true,
	"link":             true,
	"base":             true,
	"form":             true,
	"input":            true,
	"button":           true,
	"textarea":         true,
	"select":           true,
	"noscript":         true,
	"template":         true,
	"portal":           true,
	"set":              true,
	"animate":          true,
	"animatemotion":    true,
	"animatetransform": true,
}

var urlAttributes = map[string]bool{
	"href":       true,
	"src":        true,
	"action":     true,
	"formaction": true,
	"poster":     true,
	"background": true,
	"srcset":     true,
	"lowsrc":     true,
	"dynsrc":     true,
	"data":       true,
	"xlink:href": true,
}

// Sanitize returns doc as a full HTML document with every active or
// document-loading construct removed. Styles (elements and attributes), inline
// SVG and images with a data: or https: source survive — they are what makes
// the report readable.
func Sanitize(doc string) (string, error) {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return "", err
	}
	clean(root)
	ensureDoctype(root)
	var sb strings.Builder
	if err := html.Render(&sb, root); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func clean(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode && dropElement(c) {
			n.RemoveChild(c)
			c = next
			continue
		}
		if c.Type == html.ElementNode {
			c.Attr = cleanAttributes(c.Attr)
		}
		clean(c)
		c = next
	}
}

func dropElement(n *html.Node) bool {
	name := strings.ToLower(n.Data)
	if droppedElements[name] {
		return true
	}
	switch name {
	case "meta":
		return hasAttr(n, "http-equiv")
	case "img":
		return !allowedImageSource(attrValue(n, "src"))
	}
	return false
}

func cleanAttributes(attrs []html.Attribute) []html.Attribute {
	kept := attrs[:0]
	for _, a := range attrs {
		key := strings.ToLower(a.Key)
		if strings.HasPrefix(key, "on") {
			continue
		}
		qualified := key
		if a.Namespace != "" {
			qualified = strings.ToLower(a.Namespace) + ":" + key
		}
		if (urlAttributes[key] || urlAttributes[qualified]) && dangerousURL(a.Val) {
			continue
		}
		if key == "srcdoc" {
			continue
		}
		kept = append(kept, a)
	}
	return kept
}

// dangerousURL normalizes the way a browser does before checking the scheme:
// "java\tscript:" and " JavaScript:" are both javascript: to a URL parser. The
// comma case is srcset, a list of URLs where only the first is at the start.
// Any data: URL is a document the page author fully controls (text/html,
// image/svg+xml, application/xhtml+xml all run script), so only raster-or-svg
// image data — which an <img> never executes — survives.
func dangerousURL(raw string) bool {
	u := normalizeURL(raw)
	return strings.HasPrefix(u, "javascript:") ||
		strings.HasPrefix(u, "vbscript:") ||
		(strings.HasPrefix(u, "data:") && !strings.HasPrefix(u, "data:image/")) ||
		strings.Contains(u, ",javascript:") ||
		strings.Contains(u, ",data:")
}

func allowedImageSource(raw string) bool {
	u := normalizeURL(raw)
	return strings.HasPrefix(u, "https:") || strings.HasPrefix(u, "data:image/")
}

func normalizeURL(raw string) string {
	var sb strings.Builder
	for _, r := range raw {
		if r <= ' ' || r == 0x7f {
			continue
		}
		sb.WriteRune(r)
	}
	return strings.ToLower(sb.String())
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return true
		}
	}
	return false
}

func attrValue(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func ensureDoctype(root *html.Node) {
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.DoctypeNode {
			c.Data = "html"
			c.Attr = nil
			return
		}
	}
	doctype := &html.Node{Type: html.DoctypeNode, Data: "html"}
	root.InsertBefore(doctype, root.FirstChild)
}
