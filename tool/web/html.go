// Copyright 2026 Simone Vellei
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package web

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Elements with their own formatting.
const (
	tagLI  = "li"
	tagPre = "pre"
	tagA   = "a"
)

// skippedElements hold no readable text.
var skippedElements = map[string]bool{
	"script": true, "style": true, "noscript": true, "svg": true, "template": true, "iframe": true, "head": true,
}

// blockElements are paragraphs: a blank line before and after.
var blockElements = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "header": true, "footer": true, "nav": true,
	"main": true, "aside": true, "ul": true, "ol": true, "table": true, tagPre: true, "blockquote": true,
	"dl": true, "hr": true, "form": true, "figure": true,
}

// lineElements start on a new line, without a blank line.
var lineElements = map[string]bool{tagLI: true, "tr": true, "br": true, "dt": true, "dd": true}

// headingLevel returns the level of an h1 to h6 element, and 0 for any other.
func headingLevel(tag string) int {
	if len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6' { //nolint:mnd // "h" and a digit
		return int(tag[1] - '0')
	}

	return 0
}

// htmlToText returns the readable text of an HTML document: block elements
// on their own lines, headings marked with #, list items with -, links as
// "text [url]" resolved against base, and text inside pre kept as is.
func htmlToText(doc string, base *url.URL) string {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return doc
	}

	w := &textWriter{base: base}
	if title := findTitle(root); title != "" {
		w.text("# " + title)
		w.paragraphBreak()
	}

	w.walk(root)

	return w.String()
}

// textWriter accumulates the text of a document.
type textWriter struct {
	b     strings.Builder
	base  *url.URL
	pre   int
	links []int // output length where each open link started
}

func (w *textWriter) walk(n *html.Node) {
	if n.Type == html.TextNode {
		w.text(n.Data)
		return
	}

	element := n.Type == html.ElementNode
	if element && skippedElements[n.Data] {
		return
	}

	if element {
		w.open(n)
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w.walk(c)
	}

	if element {
		w.close(n)
	}
}

func (w *textWriter) open(n *html.Node) {
	w.breakFor(n)

	if level := headingLevel(n.Data); level > 0 {
		w.b.WriteString(strings.Repeat("#", level) + " ")
	}

	switch n.Data {
	case tagLI:
		w.b.WriteString("- ")
	case tagPre:
		w.pre++
	case tagA:
		w.links = append(w.links, w.b.Len())
	}
}

func (w *textWriter) close(n *html.Node) {
	switch n.Data {
	case tagA:
		start := w.links[len(w.links)-1]
		w.links = w.links[:len(w.links)-1]

		// A link with no text, such as a logo, is left out.
		href := attr(n, "href")
		if w.b.Len() > start && href != "" && !strings.HasPrefix(href, "#") && !strings.HasPrefix(href, "javascript:") {
			w.b.WriteString(" [" + w.resolve(href) + "]")
		}
	case tagPre:
		w.pre--
	}

	w.breakFor(n)
}

// breakFor ends the current line around a line element, and the current
// paragraph around a block element.
func (w *textWriter) breakFor(n *html.Node) {
	switch {
	case blockElements[n.Data], headingLevel(n.Data) > 0:
		w.paragraphBreak()
	case lineElements[n.Data]:
		w.lineBreak()
	}
}

// text writes a text node, collapsing white space outside pre.
func (w *textWriter) text(s string) {
	if w.pre > 0 {
		w.b.WriteString(s)
		return
	}

	words := strings.Fields(s)
	if len(words) == 0 {
		return
	}

	if w.b.Len() > 0 && !strings.HasSuffix(w.b.String(), "\n") && !strings.HasSuffix(w.b.String(), " ") &&
		s[0] != '.' && s[0] != ',' {
		w.b.WriteString(" ")
	}

	w.b.WriteString(strings.Join(words, " "))
}

// lineBreak ends the current line, if one is open.
func (w *textWriter) lineBreak() {
	if out := w.b.String(); out != "" && !strings.HasSuffix(out, "\n") {
		w.b.WriteString("\n")
	}
}

// paragraphBreak ends the current paragraph with one blank line, never more.
func (w *textWriter) paragraphBreak() {
	w.lineBreak()

	if out := w.b.String(); out != "" && !strings.HasSuffix(out, "\n\n") {
		w.b.WriteString("\n")
	}
}

func (w *textWriter) resolve(href string) string {
	ref, err := url.Parse(href)
	if err != nil || w.base == nil {
		return href
	}

	return w.base.ResolveReference(ref).String()
}

func (w *textWriter) String() string {
	lines := strings.Split(w.b.String(), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}

	return ""
}

// findTitle returns the text of the document's title element.
func findTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
		return strings.TrimSpace(n.FirstChild.Data)
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := findTitle(c); t != "" {
			return t
		}
	}

	return ""
}
