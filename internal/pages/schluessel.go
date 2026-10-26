package pages

import (
	"html"
	"net/http"
	"net/url"
	"strings"
)

// Keys from outside (laib's „Schlüssel hinterlegen“, owner 2026-10-26: „woher
// soll ich wissen was ich da eintragen muss und warum? … Wir sollten dahin
// verweisen von Orten wo wir sonst solche Funktionen haben“): where a
// feature waits for a key only the owner can get, Haus says what is
// missing and what it enables, and an admin gets a button straight to
// laib's page for exactly that key, which comes back here afterwards.
// The family sees that an admin has to set it up.

// ownerKey is one key from outside: its name in laib, and what it is.
type ownerKey struct{ name, label string }

// discogsKeys are the household's Discogs app (laib#696, haus#138).
var discogsKeys = []ownerKey{
	{"HAUS_DISCOGS_KEY", "Consumer Key der Discogs-App"},
	{"HAUS_DISCOGS_SECRET", "Consumer Secret der Discogs-App"},
}

// keyLink is laib's page for one key, coming back to back.
func keyLink(cfg StartConfig, name, back string) string {
	base := strings.TrimRight(cfg.LaibURL, "/")
	if base == "" {
		return ""
	}
	return base + "/schluessel?" + url.Values{"name": {name}, "zurueck": {back}}.Encode()
}

// keyHint is the note: what is not set up, what it enables, and for an
// admin the way to each key; back is where laib's page returns to.
func keyHint(r *http.Request, cfg StartConfig, what, enables, back string, keys []ownerKey) string {
	var b strings.Builder
	b.WriteString(`<div class=schluessel-fehlt role=note><p><strong>` + html.EscapeString(what) + ` ist noch nicht eingerichtet.</strong> ` + html.EscapeString(enables) + `</p>`)
	if !isAdmin(r) {
		b.WriteString(`<p class=k>Ein Admin muss das einrichten.</p></div>`)
		return b.String()
	}
	b.WriteString(`<p class=k>Dafür fehlen Schlüssel, die du als Admin bei Laib hinterlegst. Dort steht bei jedem, woher du ihn bekommst.</p><p class=wege>`)
	for _, k := range keys {
		if l := keyLink(cfg, k.name, back); l != "" {
			b.WriteString(`<a class=knopf href="` + html.EscapeString(l) + `">` + html.EscapeString(k.label) + ` hinterlegen</a> `)
		} else {
			b.WriteString(`<span class=k>` + html.EscapeString(k.label) + ` (` + html.EscapeString(k.name) + `): auf Laibs Seite unter „Schlüssel hinterlegen“</span> `)
		}
	}
	b.WriteString(`</p></div>`)
	return b.String()
}

const keyHintStyle = `<style>.schluessel-fehlt{padding:12px 14px;border-radius:14px;background:var(--accent-soft);margin:12px 0}.schluessel-fehlt p{margin:0 0 8px}.schluessel-fehlt .wege{display:flex;flex-wrap:wrap;gap:8px;margin:0}.schluessel-fehlt .knopf{display:inline-flex;align-items:center;min-height:44px;padding:0 18px;border-radius:999px;background:var(--accent-soft);color:var(--accent);font-weight:600;text-decoration:none}</style>`
