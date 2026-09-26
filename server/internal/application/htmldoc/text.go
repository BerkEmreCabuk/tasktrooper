package htmldoc

import (
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Text renders an HTML document as readable plain text: headings as "#"
// lines, lists as "-"/"1." items, table rows as "| a | b |", preformatted
// blocks fenced with ```. Markup, styles and entities are gone; diagrams
// shrink to their title. It is what an agent reads instead of the page, so a
// developer handed an analysis gets its content without the markup noise.
func Text(doc string) string {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return doc
	}
	w := &textWriter{}
	w.walk(root)
	return w.finish()
}

type listState struct {
	ordered bool
	next    int
}

type textWriter struct {
	sb      strings.Builder
	breaks  int
	space   bool
	started bool
	pre     int
	lists   []listState
}

var skippedElements = map[string]bool{
	"head": true, "style": true, "script": true, "template": true, "noscript": true, "title": true,
}

var blockElements = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "header": true, "footer": true,
	"main": true, "aside": true, "nav": true, "blockquote": true, "figure": true, "figcaption": true,
	"details": true, "summary": true, "dl": true, "address": true, "table": true, "caption": true,
	"thead": true, "tbody": true, "tfoot": true,
}

func (w *textWriter) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		w.text(n.Data)
		return
	case html.ElementNode:
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			w.walk(c)
		}
		return
	}
	name := strings.ToLower(n.Data)
	if skippedElements[name] {
		return
	}
	switch name {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level, _ := strconv.Atoi(name[1:])
		w.lineBreak(2)
		w.raw(strings.Repeat("#", level) + " ")
		w.children(n)
		w.lineBreak(2)
	case "br":
		w.lineBreak(1)
	case "hr":
		w.lineBreak(2)
		w.raw("---")
		w.lineBreak(2)
	case "ul", "ol":
		w.lineBreak(listBreak(len(w.lists)))
		w.lists = append(w.lists, listState{ordered: name == "ol", next: startAttr(n)})
		w.children(n)
		w.lists = w.lists[:len(w.lists)-1]
		w.lineBreak(listBreak(len(w.lists)))
	case "li":
		w.lineBreak(1)
		w.raw(w.listMarker())
		w.children(n)
		w.lineBreak(1)
	case "tr":
		w.lineBreak(1)
		w.raw(tableRow(n))
		w.lineBreak(1)
	case "pre":
		w.lineBreak(2)
		w.raw("```")
		w.lineBreak(1)
		w.pre++
		w.children(n)
		w.pre--
		w.lineBreak(1)
		w.raw("```")
		w.lineBreak(2)
	case "code", "kbd", "samp":
		if w.pre > 0 {
			w.children(n)
			return
		}
		w.raw("`")
		w.inline(inlineText(n))
		w.raw("`")
	case "img":
		if alt := strings.TrimSpace(attrValue(n, "alt")); alt != "" {
			w.text(" [image: " + alt + "] ")
		}
	case "svg":
		w.lineBreak(1)
		w.raw(diagramLabel(n))
		w.lineBreak(1)
	case "dt":
		w.lineBreak(1)
		w.children(n)
		w.lineBreak(1)
	case "dd":
		w.lineBreak(1)
		w.raw("  ")
		w.children(n)
		w.lineBreak(1)
	default:
		if blockElements[name] {
			w.lineBreak(2)
			w.children(n)
			w.lineBreak(2)
			return
		}
		w.children(n)
	}
}

func (w *textWriter) children(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w.walk(c)
	}
}

func listBreak(depth int) int {
	if depth == 0 {
		return 2
	}
	return 1
}

func (w *textWriter) listMarker() string {
	if len(w.lists) == 0 {
		return "- "
	}
	indent := strings.Repeat("  ", len(w.lists)-1)
	top := &w.lists[len(w.lists)-1]
	if !top.ordered {
		return indent + "- "
	}
	marker := indent + strconv.Itoa(top.next) + ". "
	top.next++
	return marker
}

func startAttr(n *html.Node) int {
	if v, err := strconv.Atoi(strings.TrimSpace(attrValue(n, "start"))); err == nil {
		return v
	}
	return 1
}

func (w *textWriter) lineBreak(n int) {
	if n > w.breaks {
		w.breaks = n
	}
	w.space = false
}

func (w *textWriter) flush() {
	if w.started && w.breaks > 0 {
		w.sb.WriteString(strings.Repeat("\n", w.breaks))
		w.space = false
	}
	w.breaks = 0
}

// raw writes markup-derived text (markers, fences) as-is, honouring a pending
// line break and a pending space but never collapsing its own whitespace.
func (w *textWriter) raw(s string) {
	if s == "" {
		return
	}
	hadBreak := w.breaks > 0
	w.flush()
	if w.space && w.started && !hadBreak {
		w.sb.WriteByte(' ')
	}
	w.space = false
	w.sb.WriteString(s)
	w.started = true
}

func (w *textWriter) inline(s string) {
	w.sb.WriteString(s)
	w.started = true
}

func (w *textWriter) text(s string) {
	if w.pre > 0 {
		w.flush()
		w.sb.WriteString(s)
		w.started = true
		return
	}
	if s == "" {
		return
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		w.space = true
		return
	}
	if isSpaceByte(s[0]) {
		w.space = true
	}
	hadBreak := w.breaks > 0
	w.flush()
	if w.space && w.started && !hadBreak && !w.endsWithSpace() {
		w.sb.WriteByte(' ')
	}
	w.sb.WriteString(strings.Join(fields, " "))
	w.started = true
	w.space = isSpaceByte(s[len(s)-1])
}

func (w *textWriter) endsWithSpace() bool {
	s := w.sb.String()
	return s != "" && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n')
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

func (w *textWriter) finish() string {
	lines := strings.Split(w.sb.String(), "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// inlineText flattens a subtree to one line — a table cell or an inline code
// span must not break the row or the backticks around it.
func inlineText(n *html.Node) string {
	var sb strings.Builder
	var collect func(*html.Node)
	collect = func(c *html.Node) {
		if c.Type == html.ElementNode && skippedElements[strings.ToLower(c.Data)] {
			return
		}
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
			sb.WriteByte(' ')
			return
		}
		if c.Type == html.ElementNode && strings.EqualFold(c.Data, "svg") {
			sb.WriteString(diagramLabel(c))
			sb.WriteByte(' ')
			return
		}
		for cc := c.FirstChild; cc != nil; cc = cc.NextSibling {
			collect(cc)
		}
	}
	collect(n)
	return strings.Join(strings.Fields(sb.String()), " ")
}

func tableRow(tr *html.Node) string {
	var cells []string
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch strings.ToLower(c.Data) {
		case "td", "th":
			cells = append(cells, strings.ReplaceAll(inlineText(c), "|", "\\|"))
		}
	}
	if len(cells) == 0 {
		return ""
	}
	return "| " + strings.Join(cells, " | ") + " |"
}

func diagramLabel(svg *html.Node) string {
	label := strings.TrimSpace(attrValue(svg, "aria-label"))
	if label == "" {
		for c := svg.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && strings.EqualFold(c.Data, "title") {
				label = inlineTextOf(c)
				break
			}
		}
	}
	if label == "" {
		return "[diagram]"
	}
	return "[diagram: " + label + "]"
}

func inlineTextOf(n *html.Node) string {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}
