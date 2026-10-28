// Package markdown renders a note's Markdown as HTML for Haus's pages
// (haus#182): no framework, the part notes use — headings, paragraphs,
// lists with tasks, quotes, code, rules, bold, italic, inline code and
// links. Everything is escaped first; HTML in a note stays text. A link
// goes out only to http(s), mailto or the page's own paths; anything else
// is shown as its text.
package markdown

import (
	"html"
	"regexp"
	"strings"
	"unicode"
)

var (
	headingRE = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	ruleRE    = regexp.MustCompile(`^\s{0,3}([-*_])(\s*[-*_]){2,}\s*$`)
	bulletRE  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	numberRE  = regexp.MustCompile(`^(\s*)(\d{1,9})[.)]\s+(.*)$`)
	taskRE    = regexp.MustCompile(`^\[([ xX])\]\s+(.*)$`)
	fenceRE   = regexp.MustCompile("^\\s*(```|~~~)")
)

// Render is text as HTML. The first level-one heading that equals title is
// left out (the page shows its title already); headings shift down one
// level, so a note's # is the page's h2.
func Render(text, title string) string {
	text = clean(text)
	lines := strings.Split(text, "\n")
	var b strings.Builder
	var para []string
	type list struct {
		ordered bool
		indent  int
	}
	var lists []list
	var quote []string
	flushPara := func() {
		if len(para) > 0 {
			b.WriteString("<p>")
			for i, l := range para {
				if i > 0 {
					b.WriteString("<br>")
				}
				b.WriteString(inline(l))
			}
			b.WriteString("</p>")
			para = nil
		}
	}
	closeLists := func(to int) {
		for len(lists) > to {
			l := lists[len(lists)-1]
			lists = lists[:len(lists)-1]
			b.WriteString("</li>")
			if l.ordered {
				b.WriteString("</ol>")
			} else {
				b.WriteString("</ul>")
			}
		}
	}
	flushQuote := func() {
		if len(quote) > 0 {
			b.WriteString("<blockquote>" + Render(strings.Join(quote, "\n"), "") + "</blockquote>")
			quote = nil
		}
	}
	skipTitle := title != ""
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if m := fenceRE.FindStringSubmatch(l); m != nil { // a code block, as it is
			flushPara()
			flushQuote()
			closeLists(0)
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), m[1]); i++ {
				code = append(code, lines[i])
			}
			b.WriteString("<pre><code>" + html.EscapeString(strings.Join(code, "\n")) + "</code></pre>")
			continue
		}
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, ">") {
			flushPara()
			closeLists(0)
			quote = append(quote, strings.TrimPrefix(strings.TrimPrefix(t, ">"), " "))
			continue
		}
		flushQuote()
		if t == "" {
			flushPara()
			if len(lists) > 0 && i+1 < len(lines) && (bulletRE.MatchString(lines[i+1]) || numberRE.MatchString(lines[i+1])) {
				continue // a loose list stays one list
			}
			closeLists(0)
			continue
		}
		if m := headingRE.FindStringSubmatch(t); m != nil {
			flushPara()
			closeLists(0)
			if skipTitle && len(m[1]) == 1 && strings.EqualFold(strings.TrimSpace(m[2]), strings.TrimSpace(title)) {
				skipTitle = false
				continue
			}
			skipTitle = false
			n := min(len(m[1])+1, 6)
			tag := "h" + string(rune('0'+n))
			b.WriteString("<" + tag + ">" + inline(m[2]) + "</" + tag + ">")
			continue
		}
		skipTitle = false
		if ruleRE.MatchString(l) && len(para) == 0 {
			closeLists(0)
			b.WriteString("<hr>")
			continue
		}
		indent, item, ordered, isItem := 0, "", false, false
		if m := bulletRE.FindStringSubmatch(l); m != nil {
			indent, item, isItem = width(m[1]), m[2], true
		} else if m := numberRE.FindStringSubmatch(l); m != nil {
			indent, item, ordered, isItem = width(m[1]), m[3], true, true
		}
		if isItem {
			flushPara()
			// deeper: a new list inside; shallower: close down to it
			for len(lists) > 0 && indent < lists[len(lists)-1].indent {
				closeLists(len(lists) - 1)
			}
			switch {
			case len(lists) == 0 || indent > lists[len(lists)-1].indent:
				lists = append(lists, list{ordered, indent})
				if ordered {
					b.WriteString("<ol>")
				} else {
					b.WriteString("<ul>")
				}
			case lists[len(lists)-1].ordered != ordered:
				closeLists(len(lists) - 1)
				lists = append(lists, list{ordered, indent})
				if ordered {
					b.WriteString("<ol>")
				} else {
					b.WriteString("<ul>")
				}
			default:
				b.WriteString("</li>")
			}
			if m := taskRE.FindStringSubmatch(item); m != nil {
				done := m[1] != " "
				b.WriteString(`<li class=aufgabe><span class="kasten` + map[bool]string{true: " an"}[done] + `" role=img aria-label="` +
					map[bool]string{true: "erledigt", false: "offen"}[done] + `"></span>` + inline(m[2]))
			} else {
				b.WriteString("<li>" + inline(item))
			}
			continue
		}
		if len(lists) > 0 && width(leading(l)) > 0 { // a list item's next line
			b.WriteString("<br>" + inline(t))
			continue
		}
		closeLists(0)
		para = append(para, t)
	}
	flushPara()
	flushQuote()
	closeLists(0)
	return b.String()
}

// clean is valid UTF-8, LF only, no control characters but tab.
func clean(text string) string {
	text = strings.ToValidUTF8(strings.ReplaceAll(text, "\r\n", "\n"), "�")
	return strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}

func leading(l string) string { return l[:len(l)-len(strings.TrimLeft(l, " \t"))] }

func width(s string) int {
	n := 0
	for _, r := range s {
		if r == '\t' {
			n += 4
		} else {
			n++
		}
	}
	return n
}

var (
	codeRE   = regexp.MustCompile("`([^`]+)`")
	linkRE   = regexp.MustCompile(`\[([^\]]+)\]\(((?:[^()\s]|\([^()\s]*\))+)\)`)
	autoRE   = regexp.MustCompile(`(^|[\s(])(https?://[^\s<>()]+[^\s<>().,;:!?'"])`)
	strongRE = regexp.MustCompile(`(\*\*|__)([^*_]+?)(\*\*|__)`)
	emRE     = regexp.MustCompile(`(^|[^\w*])[*_]([^*_\s][^*_]*?)[*_]($|[^\w*])`)
	strikeRE = regexp.MustCompile(`~~([^~]+)~~`)
)

// inline is one line's spans: code first (nothing inside is touched), then
// links, autolinks, bold, italic, strike. The text is escaped before any
// markup is added; placeholders keep code and links apart from the rest.
func inline(s string) string {
	var held []string
	hold := func(h string) string {
		held = append(held, h)
		return "\x00" + string(rune('A'+len(held)-1)) + "\x00"
	}
	s = codeRE.ReplaceAllStringFunc(s, func(m string) string {
		return hold("<code>" + html.EscapeString(m[1:len(m)-1]) + "</code>")
	})
	s = linkRE.ReplaceAllStringFunc(s, func(m string) string {
		p := linkRE.FindStringSubmatch(m)
		if !safeLink(p[2]) {
			return hold(html.EscapeString(p[1]))
		}
		return hold(`<a href="` + html.EscapeString(p[2]) + `"` + external(p[2]) + `>` + html.EscapeString(p[1]) + `</a>`)
	})
	s = autoRE.ReplaceAllStringFunc(s, func(m string) string {
		p := autoRE.FindStringSubmatch(m)
		return p[1] + hold(`<a href="`+html.EscapeString(p[2])+`" target=_blank rel=noopener>`+html.EscapeString(p[2])+`</a>`)
	})
	s = html.EscapeString(s)
	s = strongRE.ReplaceAllString(s, "<strong>$2</strong>")
	s = emRE.ReplaceAllString(s, "$1<em>$2</em>$3")
	s = strikeRE.ReplaceAllString(s, "<del>$1</del>")
	for i, h := range held {
		s = strings.Replace(s, html.EscapeString("\x00"+string(rune('A'+i))+"\x00"), h, 1)
		s = strings.Replace(s, "\x00"+string(rune('A'+i))+"\x00", h, 1)
	}
	return s
}

// safeLink is a link a note may carry: the web, mail, or the page's own paths.
func safeLink(u string) bool {
	l := strings.ToLower(u)
	return strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "mailto:") ||
		(strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//"))
}

func external(u string) string {
	if strings.HasPrefix(u, "/") {
		return ""
	}
	return " target=_blank rel=noopener"
}
