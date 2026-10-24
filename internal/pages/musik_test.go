package pages

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
