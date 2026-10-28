package markdown

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	for _, c := range []struct{ in, title, want string }{
		{"# Einkaufsliste Musik\n\nText", "Einkaufsliste Musik", "<p>Text</p>"},
		{"## Alben\n### Vinyl", "", "<h3>Alben</h3><h4>Vinyl</h4>"},
		{"eins\nzwei\n\ndrei", "", "<p>eins<br>zwei</p><p>drei</p>"},
		{"- Milch\n- Brot", "", "<ul><li>Milch</li><li>Brot</li></ul>"},
		{"1. erst\n2. dann", "", "<ol><li>erst</li><li>dann</li></ol>"},
		{"- [ ] offen\n- [x] fertig", "", `<ul><li class=aufgabe><span class="kasten" role=img aria-label="offen"></span>offen</li><li class=aufgabe><span class="kasten an" role=img aria-label="erledigt"></span>fertig</li></ul>`},
		{"- a\n  - b\n- c", "", "<ul><li>a<ul><li>b</li></ul></li><li>c</li></ul>"},
		{"**fett** und *kursiv* und `code`", "", "<p><strong>fett</strong> und <em>kursiv</em> und <code>code</code></p>"},
		{"[Wanda](https://wanda.example/x)", "", `<p><a href="https://wanda.example/x" target=_blank rel=noopener>Wanda</a></p>`},
		{"siehe https://example.org/a.", "", `<p>siehe <a href="https://example.org/a" target=_blank rel=noopener>https://example.org/a</a>.</p>`},
		{"[intern](/notizen/x.md)", "", `<p><a href="/notizen/x.md">intern</a></p>`},
		{"[böse](javascript:alert(1))", "", "<p>böse</p>"},
		{"<script>alert(1)</script> & <b>", "", "<p>&lt;script&gt;alert(1)&lt;/script&gt; &amp; &lt;b&gt;</p>"},
		{"```\n<x> **nicht fett**\n```", "", "<pre><code>&lt;x&gt; **nicht fett**</code></pre>"},
		{"> zitat\n> weiter", "", "<blockquote><p>zitat<br>weiter</p></blockquote>"},
		{"oben\n\n---\n\nunten", "", "<p>oben</p><hr><p>unten</p>"},
		{"`**x**`", "", "<p><code>**x**</code></p>"},
		{"a_b_c und snake_case", "", "<p>a_b_c und snake_case</p>"},
	} {
		if got := Render(c.in, c.title); got != c.want {
			t.Errorf("%q\n got %s\nwant %s", c.in, got, c.want)
		}
	}
	if strings.Contains(Render("[x](\"><script>)", ""), "<script>") {
		t.Error("a link breaks out")
	}
}
