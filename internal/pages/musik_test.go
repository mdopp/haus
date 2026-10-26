package pages

import (
	"bytes"
	"encoding/json"
	"github.com/mdopp/haus/internal/laib"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// musikPiece sends one piece as the page's script does.
func musikPost(t *testing.T, h http.HandlerFunc, name string, at, total int, body []byte, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	q := "?pfad=" + strings.ReplaceAll(name, " ", "%20") + "&ab=" + strconv.Itoa(at) + "&gesamt=" + strconv.Itoa(total)
	req := httptest.NewRequest("POST", MusikTeilPath+q, bytes.NewReader(body))
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// haus#107: Musik hinzufügen puts music into laib's inbox in pieces: each
// piece appended to "._<name>.part" at the offset the page says, the file
// renamed to its name after the last byte, its folders kept, never over
// another file; a piece twice answers the size there; what is not music,
// leaves the inbox, is hidden or comes from elsewhere goes nowhere. The
// page is under Mehr, and only with an inbox.
func TestMusikHinzufuegen(t *testing.T) {
	inbox := t.TempDir()
	cfg := StartConfig{MusicInbox: inbox}
	h := Musik(cfg)

	_, body, _ := familyPage(t, Musik, cfg, "GET", MusikPath, "family")
	mustContain(t, body, "<h1>Musik hinzufügen</h1>", saysMusikHow, `id=musik-dateien multiple`, `webkitdirectory`, "fetch('"+MusikTeilPath)
	mustContain(t, mehrOf(t, body), `href=/musik aria-current=page>`)
	_, body, _ = familyPage(t, Mehr, StartConfig{}, "GET", "/mehr", "family")
	mustNotContain(t, body, "Musik hinzufügen")
	if code, _, _ := familyPage(t, Musik, StartConfig{}, "GET", MusikPath, "family"); code != http.StatusNotFound {
		t.Errorf("no inbox → %d", code)
	}

	// an album folder, in two pieces
	song := bytes.Repeat([]byte("x"), 10)
	rec := musikPost(t, h, "Beatles/Abbey Road/01 Come Together.flac", 0, 10, song[:6], nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ab":6`) || !strings.Contains(rec.Body.String(), `"fertig":false`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	part := filepath.Join(inbox, "Beatles/Abbey Road/._01 Come Together.flac.part")
	if _, err := os.Stat(part); err != nil {
		t.Fatalf("no part: %v", err)
	}
	// the same piece again: the size there, nothing written twice
	rec = musikPost(t, h, "Beatles/Abbey Road/01 Come Together.flac", 0, 10, song[:6], nil)
	var a struct {
		Ab     int64
		Fertig bool
	}
	json.Unmarshal(rec.Body.Bytes(), &a)
	if rec.Code != http.StatusConflict || a.Ab != 6 {
		t.Fatalf("again: %d %s", rec.Code, rec.Body)
	}
	rec = musikPost(t, h, "Beatles/Abbey Road/01 Come Together.flac", 6, 10, song[6:], nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"fertig":true`) {
		t.Fatalf("last: %d %s", rec.Code, rec.Body)
	}
	if got, _ := os.ReadFile(filepath.Join(inbox, "Beatles/Abbey Road/01 Come Together.flac")); string(got) != string(song) {
		t.Fatalf("file %q", got)
	}
	if _, err := os.Stat(part); err == nil {
		t.Error("the part stayed")
	}
	// the same name again: beside it, not over it
	musikPost(t, h, "Beatles/Abbey Road/01 Come Together.flac", 0, 3, []byte("neu"), nil)
	if got, _ := os.ReadFile(filepath.Join(inbox, "Beatles/Abbey Road/01 Come Together (2).flac")); string(got) != "neu" {
		t.Fatalf("second %q", got)
	}
	// a ZIP from the phone, in one piece
	if rec := musikPost(t, h, "album.zip", 0, 4, []byte("PK.."), nil); rec.Code != 200 {
		t.Fatalf("zip: %d", rec.Code)
	}

	for _, bad := range []string{"../draussen.mp3", "/etc/x.mp3", "a/.versteckt/x.mp3", "notiz.txt", "a\\b.mp3", "", "a/../../x.mp3"} {
		if rec := musikPost(t, h, bad, 0, 1, []byte("x"), nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%q → %d", bad, rec.Code)
		}
	}
	if rec := musikPost(t, h, "x.mp3", 0, 1, []byte("x"), map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site → %d", rec.Code)
	}
	if rec := musikPost(t, h, "y.mp3", 0, 2, []byte("xyz"), nil); rec.Code != http.StatusBadRequest {
		t.Errorf("more than announced → %d", rec.Code)
	}
	if rec := musikPost(t, h, "z.mp3", 0, int(musikMaxFile)+1, []byte("x"), nil); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("too big → %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(inbox), "draussen.mp3")); err == nil {
		t.Fatal("wrote outside the inbox")
	}
	for _, f := range []string{"x.mp3", "y.mp3", "._y.mp3.part"} {
		if _, err := os.Stat(filepath.Join(inbox, f)); err == nil {
			t.Errorf("%s written", f)
		}
	}
}

// haus#113: who uploaded goes beside the upload at the inbox's top before
// it is whole (laib's beets contract), so the importer's message goes to
// them; after the upload the page shows laib's line of the import.
func TestMusikSaysWhoAndHowFar(t *testing.T) {
	inbox := t.TempDir()
	h := Musik(StartConfig{MusicInbox: inbox})
	musikPost(t, h, "Beatles/Abbey Road/01 Come Together.flac", 0, 3, []byte("abc"), nil)
	musikPost(t, h, "lied.mp3", 0, 3, []byte("abc"), nil)
	musikPost(t, h, "lied.mp3", 0, 3, []byte("abc"), nil) // beside it: lied (2).mp3
	for name, want := range map[string]string{"._Beatles.von": "anna\n", "._lied.mp3.von": "anna\n", "._lied (2).mp3.von": "anna\n"} {
		if got, err := os.ReadFile(filepath.Join(inbox, name)); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", name, got, err)
		}
	}
	if rec := musikPost(t, h, "x.mp3", 0, 1, []byte("x"), map[string]string{"Remote-User": "Anna Müller"}); rec.Code != 200 {
		t.Fatalf("%d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(inbox, "._x.mp3.von")); err == nil {
		t.Error("a name the importer does not take was written")
	}
	for in, want := range map[string]string{"anna": "anna", "Erika": "erika", "a b": "", "": "", strings.Repeat("a", 65): "", "ben.k@x-y_z": "ben.k@x-y_z", "ä": ""} {
		if got := uploader(in); got != want {
			t.Errorf("uploader(%q) = %q", in, got)
		}
	}

	line, wartet := importLine("Musik-Eingang: zuletzt 12 Titel (2 Alben) importiert · 1 Eingang wird noch kopiert · geprüft vor 2 min")
	if line != "zuletzt 12 Titel (2 Alben) importiert" || !wartet {
		t.Errorf("%q %v", line, wartet)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := json.Marshal(map[string]any{"services": []map[string]any{{"name": "beets", "state": "grün", "detail": "Musik-Eingang: zuletzt 3 Titel importiert · geprüft gerade eben"}}})
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": string(b)}}}})
	}))
	defer srv.Close()
	code, body, hdr := familyPage(t, Musik, StartConfig{MusicInbox: inbox, Laib: &laib.Client{URL: srv.URL, Token: "t"}}, "GET", MusikStandPath, "family")
	if code != 200 || hdr.Get("Content-Type") != "application/json" || strings.TrimSpace(body) != `{"ergebnis":[],"stand":"zuletzt 3 Titel importiert","wartet":false}` {
		t.Errorf("%d %q", code, body)
	}
	_, body, _ = familyPage(t, Musik, StartConfig{MusicInbox: inbox}, "GET", MusikStandPath, "family")
	if strings.TrimSpace(body) != `{"ergebnis":[],"stand":"","wartet":false}` {
		t.Errorf("without laib: %q", body)
	}
}

// haus#107 (laib#688): after the upload the page says what became of each
// upload — its titles and albums, or what was kept back and why — read
// from beets' own status, only for these uploads since the page started.
func TestMusikSaysWhatBecameOfIt(t *testing.T) {
	own := json.RawMessage(`{"letzte":[
		{"at":"2026-10-26T12:05:00Z","eingang":"KREIS.zip","titel":0,"alben":0,"nicht_importiert":[{"datei":"KREIS.zip","grund":"unsicherer Pfad in der ZIP-Datei"}]},
		{"at":"2026-10-26T12:04:00Z","eingang":"Ein Lächeln im Gesicht","titel":13,"alben":1,"namen":["Farin Urlaub – Ein Lächeln im Gesicht"]},
		{"at":"2026-10-26T12:04:30Z","eingang":"lied.mp3","titel":1,"alben":0},
		{"at":"2026-10-20T10:00:00Z","eingang":"Ein Lächeln im Gesicht","titel":2}]}`)
	got := uploadResults(own, []string{"Ein Lächeln im Gesicht", "KREIS.zip"}, "2026-10-26T12:00:00Z")
	want := []Ergebnis{
		{Eingang: "KREIS.zip", Satz: "KREIS.zip: nicht übernommen: KREIS.zip (unsicherer Pfad in der ZIP-Datei)."},
		{Eingang: "Ein Lächeln im Gesicht", Satz: "Ein Lächeln im Gesicht: 13 Titel (1 Album): Farin Urlaub – Ein Lächeln im Gesicht in der Musik.", Gut: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%#v", got)
	}
	if got := uploadResults(nil, []string{"x"}, "2026-10-26T12:00:00Z"); len(got) != 0 {
		t.Errorf("nothing said: %v", got)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := json.Marshal(map[string]any{"services": []map[string]any{{"name": "beets", "state": "grün", "detail": "Musik-Eingang: zuletzt 13 Titel importiert", "eigenes": own}}})
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": string(b)}}}})
	}))
	defer srv.Close()
	_, body, _ := familyPage(t, Musik, StartConfig{MusicInbox: t.TempDir(), Laib: &laib.Client{URL: srv.URL, Token: "t"}}, "GET",
		MusikStandPath+"?seit=2026-10-26T12:00:00Z&namen=lied.mp3", "family")
	if !strings.Contains(body, `"ergebnis":[{"eingang":"lied.mp3","satz":"lied.mp3: 1 Titel in der Musik.","gut":true}]`) {
		t.Errorf("%s", body)
	}
}
