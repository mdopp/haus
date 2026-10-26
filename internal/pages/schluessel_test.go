package pages

import (
	"context"
	"net/http"
	"testing"

	"github.com/mdopp/haus/internal/einkauf"
)

// owner 2026-10-26 (via laib): where a feature waits for a key from
// outside, say what is missing and what it enables; an admin goes straight
// to laib's page for that one key, which comes back; the family is told
// an admin has to set it up.
func TestDiscogsWithoutKeysSaysHow(t *testing.T) {
	l, _ := shoppingList(t)
	l.Changed = nil
	cfg := StartConfig{Einkauf: l, Wuensche: &einkauf.Wuensche{Listen: l, Dir: t.TempDir()}, LaibURL: "https://admin.box.test/", PublicURL: "https://haus.box.test"}
	cfg.Wuensche.Ensure(context.Background())
	code, body, _ := familyPage(t, Discogs, cfg, "GET", DiscogsPath, "admins")
	if code != http.StatusOK {
		t.Fatalf("→ %d", code)
	}
	mustContain(t, body, "Discogs ist noch nicht eingerichtet.", "Wantlist",
		`href="https://admin.box.test/schluessel?name=HAUS_DISCOGS_KEY&amp;zurueck=https%3A%2F%2Fhaus.box.test%2Fdiscogs">Consumer Key der Discogs-App hinterlegen</a>`,
		`href="https://admin.box.test/schluessel?name=HAUS_DISCOGS_SECRET&amp;zurueck=https%3A%2F%2Fhaus.box.test%2Fdiscogs">Consumer Secret der Discogs-App hinterlegen</a>`)
	mustNotContain(t, body, "Mit Discogs verbinden")

	_, body, _ = familyPage(t, Discogs, cfg, "GET", DiscogsPath, "family")
	mustContain(t, body, "Discogs ist noch nicht eingerichtet.", "Ein Admin muss das einrichten.")
	mustNotContain(t, body, "/schluessel")

	_, body, _ = familyPage(t, Einkauf, cfg, "GET", "/einkauf?liste=liste-musik", "admins")
	mustContain(t, body, "Discogs ist noch nicht eingerichtet.", "zurueck=https%3A%2F%2Fhaus.box.test%2Feinkauf%3Fliste%3Dliste-musik")
	_, body, _ = familyPage(t, Einkauf, cfg, "GET", "/einkauf", "admins")
	mustNotContain(t, body, "Discogs ist noch nicht eingerichtet.")

	_, body, _ = familyPage(t, Mehr, cfg, "GET", mehrPath, "family")
	mustContain(t, body, "Discogs")

	cfg.LaibURL = "" // no laib page known: where to find it, in words
	_, body, _ = familyPage(t, Discogs, cfg, "GET", DiscogsPath, "admins")
	mustContain(t, body, "(HAUS_DISCOGS_KEY): auf Laibs Seite unter „Schlüssel hinterlegen“")
}
