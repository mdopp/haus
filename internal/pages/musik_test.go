package pages

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/laib"
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

// Musik verwalten (owner 2026-11-04: „nur Dateien darstellen, aber nichts Komplexes“): one h1,
// Hinzufügen with one list of the files under it — being sorted in, not sorted in with its
// reason and actions on its row, the last week's done — then Platten.
func TestMusikVerwalten(t *testing.T) {
	inbox := t.TempDir()
	now := time.Date(2026, 11, 4, 18, 0, 0, 0, time.UTC)
	run := filepath.Join(inbox, "_nicht-importiert", "2026-11-04_17-20-00")
	os.MkdirAll(run, 0o755)
	os.WriteFile(filepath.Join(run, "kassette.rar"), []byte("Rar!"), 0o644)
	os.WriteFile(filepath.Join(run, "GRUND.txt"), []byte("kassette.rar: RAR kann die Box nicht auspacken\n"), 0o644)
	os.WriteFile(filepath.Join(inbox, "Sommer.zip"), []byte("PK"), 0o644)
	os.Chtimes(filepath.Join(inbox, "Sommer.zip"), now.Add(-time.Minute), now.Add(-time.Minute))
	os.WriteFile(filepath.Join(inbox, "._Sommer.zip.von"), []byte("anna\n"), 0o644) // a sidecar is no file of the list
	own := fmt.Sprintf(`{"wartet":1,"letzte":[{"at":%q,"eingang":"kassette.rar","nicht_importiert":[{"datei":"kassette.rar","grund":"RAR"}]},
		{"at":%q,"eingang":"Album.zip","titel":24,"alben":2},{"at":"2026-10-01T10:00:00Z","eingang":"alt.mp3","titel":1}]}`,
		now.Add(-40*time.Minute).Format(time.RFC3339), now.Add(-3*time.Hour).Format(time.RFC3339))
	cfg := StartConfig{MusicInbox: inbox, Laib: beetsLaib(t, own), Samba: &samba.Dir{Path: t.TempDir()}, Loc: time.UTC, Now: func() time.Time { return now }}
	_, body, _ := familyPage(t, Musik, cfg, "GET", MusikPath, "family")
	if got := strings.Join(headings(body), " | "); got != "h1 Musik | h2 Hinzufügen" {
		t.Errorf("headings %q", got)
	}
	mustContain(t, body, "Auf dem Handy: Lieder", `auf dem <a href=/netzlaufwerk>Netzlaufwerk</a> legen.`,
		`<li><span class="punkt gelb"></span><div><span class=name>Sommer.zip</span><span class=k>wird einsortiert …</span></div></li>`,
		`<li><span class="punkt rot"></span><div><span class=name>kassette.rar</span><span class=k>nicht einsortiert: RAR kann die Box nicht auspacken</span><div class=musik-tun>`,
		`<button data-laeuft="Legt zurück …">Noch einmal einsortieren</button>`, `<button class=weg data-laeuft="Löscht …">Löschen</button>`,
		`<li><span class="punkt "></span><div><span class=name>Album.zip</span><span class=k>fertig: 2 Alben, 24 Titel in der Musik · heute, 15:00</span></div></li>`,
		`<span hidden data-auffrischen=10></span>`)
	mustNotContain(t, body, "alt.mp3", "Sommer.zip.von", "musik-dateien", "webkitdirectory", "Zuletzt hinzugefügt", "Braucht Aufmerksamkeit", "Gelöscht")
	order := []string{"id=m-h", "Auf dem Handy", "punkt gelb", "punkt rot", "Album.zip"}
	for i := 1; i < len(order); i++ {
		if strings.Index(body, order[i-1]) > strings.Index(body, order[i]) {
			t.Errorf("%s after %s", order[i-1], order[i])
		}
	}
	now = now.Add(20 * time.Minute) // waiting long: said so
	_, body, _ = familyPage(t, Musik, cfg, "GET", MusikPath, "family")
	mustContain(t, body, "wird einsortiert … Das dauert länger als sonst")
}

// Nothing uploaded, no Laib, no network drive: Hinzufügen alone, the phone's way only, an
// empty list, and nothing refreshes.
func TestMusikLeer(t *testing.T) {
	for _, cfg := range []StartConfig{{MusicInbox: t.TempDir()}, {MusicInbox: t.TempDir(), Laib: beetsLaib(t, `{"letzte":[]}`)}} {
		_, body, _ := familyPage(t, Musik, cfg, "GET", MusikPath, "family")
		if got := strings.Join(headings(body), " | "); got != "h1 Musik | h2 Hinzufügen" {
			t.Errorf("headings %q", got)
		}
		mustContain(t, body, "Auf dem Handy: Lieder", "<ul class=musik-liste></ul>")
		mustNotContain(t, body, "Netzlaufwerk", "<span hidden data-auffrischen", "Platten")
	}
	_, body, _ := familyPage(t, Musik, StartConfig{MusicInbox: t.TempDir(), Module: modulListe(t, "musik")}, "GET", MusikPath, "family")
	mustContain(t, mehrOf(t, body), `href=/musik aria-current=page>`)
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
	mustContain(t, body, "<span class=name>KREIS.zip</span><span class=k>nicht einsortiert: unsicherer Pfad in der ZIP-Datei (absolut): /</span>",
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
	mustContain(t, rec.Body.String(), "„KREIS.zip“ liegt wieder im Musik-Eingang.", `<span class=name>KREIS.zip</span><span class=k>wird einsortiert …</span>`, "<span hidden data-auffrischen")
	mustNotContain(t, rec.Body.String(), "nicht einsortiert")
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
	mustContain(t, body, "<span class=name>KREIS</span><span class=k>nicht einsortiert: folder.jpg: keine Musikdatei; info.nfo: keine Musikdatei</span>")
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
	mustContain(t, body, "„Album“ ist gelöscht.", `name=tun value=zurueckholen>`, ">Rückgängig</button>")
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
	mustContain(t, rec.Body.String(), "„Album“ ist wieder da.", "<span class=name>Album</span>")
	if b, _ := os.ReadFile(filepath.Join(run, "Album", "01.mp3")); string(b) != "x" {
		t.Fatal("not back")
	}
	if rec := post(t, h, MusikPath, url.Values{"tun": {"zurueckholen"}, "id": {es[0].ID}}, "anna", nil); rec.Code != http.StatusConflict {
		t.Errorf("twice → %d", rec.Code)
	}

	// after 30 days it is gone, files too
	post(t, h, MusikPath, url.Values{"tun": {"loeschen"}, "lauf": {"2026-10-25_09-00-00"}, "name": {"Album"}}, "anna", nil)
	now = now.Add(31 * 24 * time.Hour)
	familyPage(t, Musik, cfg, "GET", MusikPath, "family") // a look at the page lets go of what is older than 30 days
	if ds, _ := os.ReadDir(filepath.Join(inbox, ".papierkorb")); len(ds) != 0 {
		t.Errorf("old files stay: %v", ds)
	}
}
