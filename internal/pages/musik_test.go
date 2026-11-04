package pages

import (
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/jellyfin"
	"github.com/mdopp/homes/internal/jellyfin/jellyfinfake"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/papierkorb"
	"github.com/mdopp/homes/internal/samba"
)

// beetsLaib is a Laib whose status of beets says own; "" says nothing of it.
func beetsLaib(t *testing.T, own string) *laib.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		svc := []map[string]any{}
		if own != "" {
			svc = append(svc, map[string]any{"name": "beets", "detail": "Musik-Eingang: …", "eigenes": json.RawMessage(own)})
		}
		b, _ := json.Marshal(map[string]any{"services": svc})
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": string(b)}}}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

// headings are a page's h1 and h2 in order, "h1 Musik".
func headings(body string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`<(h[12])[^>]*>([^<]*)</h[12]>`).FindAllStringSubmatch(body, -1) {
		out = append(out, m[1]+" "+m[2])
	}
	return out
}

// Musik verwalten (owner 2026-11-04): one h1, then Hinzufügen with the import's state under
// it, Zuletzt hinzugefügt, Braucht Aufmerksamkeit, Platten, and Discogs quiet last — each from
// real data: beets' status from Laib, the inbox, Jellyfin's newest albums.
func TestMusikVerwalten(t *testing.T) {
	inbox := t.TempDir()
	now := time.Date(2026, 11, 4, 18, 0, 0, 0, time.UTC)
	run := filepath.Join(inbox, "_nicht-importiert", "2026-11-04_17-20-00")
	os.MkdirAll(run, 0o755)
	os.WriteFile(filepath.Join(run, "kassette.rar"), []byte("Rar!"), 0o644)
	os.WriteFile(filepath.Join(run, "GRUND.txt"), []byte("kassette.rar: RAR kann die Box nicht auspacken\n"), 0o644)
	os.WriteFile(filepath.Join(inbox, "Sommer.zip"), []byte("PK"), 0o644)
	os.Chtimes(filepath.Join(inbox, "Sommer.zip"), now.Add(-time.Minute), now.Add(-time.Minute))
	os.WriteFile(filepath.Join(inbox, "._Sommer.zip.von"), []byte("anna\n"), 0o644) // a sidecar is no upload
	own := fmt.Sprintf(`{"wartet":1,"letzte":[{"at":%q,"eingang":"kassette.rar","nicht_importiert":[{"datei":"kassette.rar","grund":"RAR kann die Box nicht auspacken"}]},
		{"at":%q,"eingang":"Album.zip","titel":24,"alben":2},{"at":"2026-10-01T10:00:00Z","eingang":"alt.mp3","titel":1}]}`,
		now.Add(-40*time.Minute).Format(time.RFC3339), now.Add(-3*time.Hour).Format(time.RFC3339))
	jf := jellyfinfake.New()
	defer jf.Close()
	jf.Setze(jellyfinfake.Album{ID: strings.Repeat("a", 32), Name: "Ciao!", Kuenstler: "Wanda", Farbe: color.RGBA{200, 100, 50, 255}},
		jellyfinfake.Album{ID: strings.Repeat("b", 32), Name: "Ohne Bild", Kuenstler: "X"})
	filme, _ := modul.Bauen([]modul.Modul{{Name: "musik", Nav: Nav("musik")}, {Name: "filme", Einrichtung: &modul.Einrichtung{Web: "https://media.example"}}})
	cfg := StartConfig{MusicInbox: inbox, Laib: beetsLaib(t, own), Musikbib: &jellyfin.Client{URL: jf.URL, Token: "k"}, Module: filme,
		Samba: &samba.Dir{Path: t.TempDir()}, Loc: time.UTC, Now: func() time.Time { return now }}
	_, body, _ := familyPage(t, Musik, cfg, "GET", MusikPath, "family")
	if got, want := strings.Join(headings(body), " | "), "h1 Musik | h2 Hinzufügen | h2 Zuletzt hinzugefügt | h2 Braucht Aufmerksamkeit"; got != want {
		t.Errorf("headings %q, want %q", got, want)
	}
	mustContain(t, body, "Auf dem Handy: Lieder", `auf dem <a href=/netzlaufwerk>Netzlaufwerk</a> legen.`,
		`<span class="punkt gelb"></span>Wird einsortiert: „Sommer.zip“.</p><span hidden data-auffrischen=10></span>`,
		`<span class="punkt rot"></span>Nicht einsortiert: „kassette.rar“ – RAR kann die Box nicht auspacken · heute, 17:20. Was jetzt: unten unter „Braucht Aufmerksamkeit“.`,
		`<span class="punkt "></span>Fertig: „Album.zip“, 2 Alben, 24 Titel in der Musik · heute, 15:00`,
		"24 Alben in der Musik", `<a href="https://media.example/web/#/details?id=`+strings.Repeat("a", 32)+`"><img class=bild src="/musik/bild?id=`+strings.Repeat("a", 32)+`"`,
		`<span class=name>Ciao!</span><span class=wer>Wanda</span>`, `<span class=bild aria-hidden=true><svg`, "<span class=name>kassette.rar</span>")
	mustNotContain(t, body, "alt.mp3", "Sommer.zip.von", "musik-dateien", "webkitdirectory")
	order := []string{"id=m-h", "punkt gelb", "punkt rot", "id=m-z", "id=m-n"}
	for i := 1; i < len(order); i++ {
		if strings.Index(body, order[i-1]) > strings.Index(body, order[i]) {
			t.Errorf("%s after %s", order[i-1], order[i])
		}
	}
	// the cover through Homes; an id that is none, 404
	code, png, hdr := familyPage(t, Musik, cfg, "GET", MusikBildPath+"?id="+strings.Repeat("a", 32), "family")
	if code != 200 || hdr.Get("Content-Type") != "image/png" || !strings.HasPrefix(png, "\x89PNG") {
		t.Errorf("cover: %d %s", code, hdr.Get("Content-Type"))
	}
	for _, id := range []string{"../x", strings.Repeat("b", 32), ""} {
		if code, _, _ := familyPage(t, Musik, cfg, "GET", MusikBildPath+"?id="+url.QueryEscape(id), "family"); code != http.StatusNotFound {
			t.Errorf("cover %q → %d", id, code)
		}
	}

	// waiting long: said so; Jellyfin silent: a line, no tiles
	now = now.Add(20 * time.Minute)
	jf.Close()
	_, body, _ = familyPage(t, Musik, cfg, "GET", MusikPath, "family")
	mustContain(t, body, "Das dauert länger als sonst", "Was zuletzt dazukam, lässt sich gerade nicht nachsehen")
	mustNotContain(t, body, "<ul class=alben>")
}

// An empty library, nothing uploaded, no Laib, no Jellyfin, no network drive: Hinzufügen
// alone, the phone's way only; nothing that has no data is shown, and nothing refreshes.
func TestMusikLeer(t *testing.T) {
	jf := jellyfinfake.New()
	defer jf.Close()
	for _, cfg := range []StartConfig{{MusicInbox: t.TempDir()}, {MusicInbox: t.TempDir(), Laib: beetsLaib(t, `{"letzte":[]}`), Musikbib: &jellyfin.Client{URL: jf.URL, Token: "k"}}} {
		_, body, _ := familyPage(t, Musik, cfg, "GET", MusikPath, "family")
		if got := strings.Join(headings(body), " | "); got != "h1 Musik | h2 Hinzufügen" {
			t.Errorf("headings %q", got)
		}
		mustContain(t, body, "Auf dem Handy: Lieder")
		mustNotContain(t, body, "Netzlaufwerk", "class=zustand", "<span hidden data-auffrischen", "Zuletzt hinzugefügt", "Braucht Aufmerksamkeit", "Gelöscht", "Platten")
	}
	mustContain(t, mehrOf(t, func() string {
		_, b, _ := familyPage(t, Musik, StartConfig{MusicInbox: t.TempDir(), Module: modulListe(t, "musik")}, "GET", MusikPath, "family")
		return b
	}()), `href=/musik aria-current=page>`)
	if code, _, _ := familyPage(t, Musik, StartConfig{}, "GET", MusikPath, "family"); code != http.StatusNotFound {
		t.Errorf("no inbox → %d", code)
	}
}

// owner 2026-10-26: what beets kept back can go into the inbox again — a
// ZIP refused before laib's fix to its path check is taken now.
func TestMusikNochmal(t *testing.T) {
	inbox := t.TempDir()
	run := filepath.Join(inbox, "_nicht-importiert", "2026-10-24_18-11-01")
	os.MkdirAll(run, 0o755)
	os.WriteFile(filepath.Join(run, "KREIS.zip"), []byte("PK.."), 0o644)
	os.WriteFile(filepath.Join(run, "GRUND.txt"), []byte("Musik-Eingang, 2026-10-24\nDiese Dateien …\n\nKREIS.zip: unsicherer Pfad in der ZIP-Datei (absolut): /\n"), 0o644)
	cfg := StartConfig{MusicInbox: inbox}
	_, body, _ := familyPage(t, Musik, cfg, "GET", MusikPath, "family")
	mustContain(t, body, "<h2 id=m-n>Braucht Aufmerksamkeit</h2>", "<span class=name>KREIS.zip</span><span class=k>unsicherer Pfad in der ZIP-Datei (absolut): /</span>",
		`<input type=hidden name=lauf value="2026-10-24_18-11-01"><input type=hidden name=name value="KREIS.zip"><button data-laeuft="Legt zurück …">Noch einmal einsortieren</button>`)

	h := Musik(cfg)
	for _, bad := range []url.Values{{"tun": {"nochmal"}, "lauf": {".."}, "name": {"KREIS.zip"}}, {"tun": {"nochmal"}, "lauf": {"2026-10-24_18-11-01"}, "name": {"../x"}},
		{"tun": {"nochmal"}, "lauf": {"2026-10-24_18-11-01"}, "name": {"GRUND.txt"}}} {
		if rec := post(t, h, MusikPath, bad, "anna", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%v → %d", bad, rec.Code)
		}
	}
	if rec := post(t, h, MusikPath, url.Values{"tun": {"nochmal"}, "lauf": {"2026-10-24_18-11-01"}, "name": {"KREIS.zip"}}, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site → %d", rec.Code)
	}
	rec := post(t, h, MusikPath, url.Values{"tun": {"nochmal"}, "lauf": {"2026-10-24_18-11-01"}, "name": {"KREIS.zip"}}, "anna", nil)
	mustContain(t, rec.Body.String(), "„KREIS.zip“ liegt wieder im Musik-Eingang.", `Wird einsortiert: „KREIS.zip“.`, "data-auffrischen")
	mustNotContain(t, rec.Body.String(), "Braucht Aufmerksamkeit")
	if b, _ := os.ReadFile(filepath.Join(inbox, "KREIS.zip")); string(b) != "PK.." {
		t.Errorf("not in the inbox: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(inbox, "._KREIS.zip.von")); string(b) != "anna\n" {
		t.Errorf("who: %q", b)
	}
	if _, err := os.Stat(run); err == nil {
		t.Error("the empty run stayed")
	}
	if rec := post(t, h, MusikPath, url.Values{"tun": {"nochmal"}, "lauf": {"2026-10-24_18-11-01"}, "name": {"KREIS.zip"}}, "anna", nil); rec.Code != http.StatusConflict {
		t.Errorf("twice → %d", rec.Code)
	}
}

// owner 2026-10-26: "Wenn ich sie einsortieren kann, sollte ich die auch
// löschen können" — asked first, then gone, a folder too.
func TestMusikLoeschen(t *testing.T) {
	inbox := t.TempDir()
	run := filepath.Join(inbox, "_nicht-importiert", "2026-10-25_09-00-00")
	os.MkdirAll(filepath.Join(run, "Kaputtes Album"), 0o755)
	os.WriteFile(filepath.Join(run, "Kaputtes Album", "01.mp3"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(run, "rest.mp3"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(run, "GRUND.txt"), []byte("Kaputtes Album: beschädigt\n"), 0o644)
	cfg := StartConfig{MusicInbox: inbox}
	_, body, _ := familyPage(t, Musik, cfg, "GET", MusikPath, "family")
	mustContain(t, body, `data-frage="„Kaputtes Album“ endgültig löschen?"><input type=hidden name=tun value=loeschen>`, `<button class=weg data-laeuft="Löscht …">Löschen</button>`)
	h := Musik(cfg)
	rec := post(t, h, MusikPath, url.Values{"tun": {"loeschen"}, "lauf": {"2026-10-25_09-00-00"}, "name": {"Kaputtes Album"}}, "anna", nil)
	mustContain(t, rec.Body.String(), "„Kaputtes Album“ ist gelöscht.", "rest.mp3")
	if _, err := os.Stat(filepath.Join(run, "Kaputtes Album")); err == nil {
		t.Error("still there")
	}
	post(t, h, MusikPath, url.Values{"tun": {"loeschen"}, "lauf": {"2026-10-25_09-00-00"}, "name": {"rest.mp3"}}, "anna", nil)
	if _, err := os.Stat(run); err == nil {
		t.Error("the empty run stayed")
	}
	if rec := post(t, h, MusikPath, url.Values{"tun": {"loeschen"}, "lauf": {".."}, "name": {"musik"}}, "anna", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("outside → %d", rec.Code)
	}
}

// beets names a file inside an upload's folder ("KREIS/folder.jpg: keine
// Musikdatei"); the folder's line says it (seen on the box, 2026-10-26).
func TestMusikReasonOfAFolder(t *testing.T) {
	inbox := t.TempDir()
	run := filepath.Join(inbox, "_nicht-importiert", "2026-10-26_17-45-00")
	os.MkdirAll(filepath.Join(run, "KREIS"), 0o755)
	os.WriteFile(filepath.Join(run, "KREIS", "folder.jpg"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(run, "GRUND.txt"), []byte("Musik-Eingang\n\nKREIS/folder.jpg: keine Musikdatei\nKREIS/info.nfo: keine Musikdatei\n"), 0o644)
	_, body, _ := familyPage(t, Musik, StartConfig{MusicInbox: inbox}, "GET", MusikPath, "family")
	mustContain(t, body, "<span class=name>KREIS</span><span class=k>folder.jpg: keine Musikdatei; info.nfo: keine Musikdatei</span>")
}

// haus#174: a deleted kept-back upload waits 30 days in the inbox's hidden
// Papierkorb (the importer never touches a dot folder); Rückgängig puts it back
func TestMusikLoeschenRueckgaengig(t *testing.T) {
	inbox := t.TempDir()
	run := filepath.Join(inbox, "_nicht-importiert", "2026-10-25_09-00-00")
	os.MkdirAll(filepath.Join(run, "Album"), 0o755)
	os.WriteFile(filepath.Join(run, "Album", "01.mp3"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(run, "GRUND.txt"), []byte("Album: beschädigt\n"), 0o644)
	now := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	cfg := StartConfig{MusicInbox: inbox, Papierkorb: &papierkorb.Korb{Dir: t.TempDir(), Now: func() time.Time { return now }}}
	h := Musik(cfg)
	rec := post(t, h, MusikPath, url.Values{"tun": {"loeschen"}, "lauf": {"2026-10-25_09-00-00"}, "name": {"Album"}}, "anna", nil)
	body := rec.Body.String()
	mustContain(t, body, "„Album“ ist gelöscht.", `name=tun value=zurueckholen>`, ">Rückgängig</button>", "<h2 id=m-g>Gelöscht</h2>", "bis 27.11.")
	if _, err := os.Stat(filepath.Join(run, "Album")); err == nil {
		t.Fatal("still under Nicht übernommen")
	}
	es := cfg.Papierkorb.Liste("musik")
	if len(es) != 1 {
		t.Fatalf("papierkorb: %v", es)
	}
	if b, _ := os.ReadFile(filepath.Join(inbox, ".papierkorb", es[0].ID, "Album", "01.mp3")); string(b) != "x" {
		t.Fatal("not kept in the hidden Papierkorb")
	}
	rec = post(t, h, MusikPath, url.Values{"tun": {"zurueckholen"}, "id": {es[0].ID}}, "anna", nil)
	mustContain(t, rec.Body.String(), "„Album“ ist wieder da, unter „Braucht Aufmerksamkeit“.", "<span class=name>Album</span>")
	if b, _ := os.ReadFile(filepath.Join(run, "Album", "01.mp3")); string(b) != "x" {
		t.Fatal("not back")
	}
	if rec := post(t, h, MusikPath, url.Values{"tun": {"zurueckholen"}, "id": {es[0].ID}}, "anna", nil); rec.Code != http.StatusConflict {
		t.Errorf("twice → %d", rec.Code)
	}

	// after 30 days it is gone, files too
	post(t, h, MusikPath, url.Values{"tun": {"loeschen"}, "lauf": {"2026-10-25_09-00-00"}, "name": {"Album"}}, "anna", nil)
	now = now.Add(31 * 24 * time.Hour)
	_, body, _ = familyPage(t, Musik, cfg, "GET", MusikPath, "family")
	mustNotContain(t, body, "<h2 id=m-g>Gelöscht</h2>")
	if ds, _ := os.ReadDir(filepath.Join(inbox, ".papierkorb")); len(ds) != 0 {
		t.Errorf("old files stay: %v", ds)
	}
}
