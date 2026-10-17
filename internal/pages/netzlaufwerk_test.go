package pages

import (
	"bytes"
	"encoding/base64"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/samba"
	"github.com/mdopp/haus/internal/samba/sambafake"
)

// haus#52, laib#296: each person sets their own network-drive password on
// /netzlaufwerk; an admin can reset anyone's. file-share's end is a fake
// with a real FIFO.
func netzlaufwerk(t *testing.T, confirm bool) (*sambafake.FileShare, StartConfig) {
	t.Helper()
	fs := sambafake.New(t, map[string]int64{"anna": 0, "ben": 1777777777, "core": 0}, confirm)
	return fs, StartConfig{Samba: &samba.Dir{Path: fs.Dir}, SambaHost: "192.168.1.10"}
}

func getNetzlaufwerk(cfg StartConfig, user, groups, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/netzlaufwerk"+query, nil)
	req.Header.Set("Remote-User", user)
	req.Header.Set("Remote-Groups", groups)
	rec := httptest.NewRecorder()
	Netzlaufwerk(cfg).ServeHTTP(rec, req)
	return rec
}

func TestNetzlaufwerkSaysWhatItIsAndWhetherYouHaveOne(t *testing.T) {
	_, cfg := netzlaufwerk(t, true)
	rec := getNetzlaufwerk(cfg, "anna", "family", "")
	page := rec.Body.String()
	for _, want := range []string{"<h1>Netzlaufwerk</h1>", "Windows", "Mac", "Dateien-App", "Noch kein Passwort",
		"dasselbe sein wie dein Familien-Passwort", `\\192.168.1.10\data`, "smb://192.168.1.10/data", "type=password", "autocomplete=new-password", ">anna<"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" || strings.Contains(page, "Passwörter der Familie") {
		t.Fatalf("%d %q; only an admin sees the family's", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if page := getNetzlaufwerk(cfg, "ben", "family", "").Body.String(); !strings.Contains(page, "Passwort gesetzt am") || strings.Contains(page, "Noch kein Passwort") {
		t.Errorf("ben has one:\n%s", page)
	}
	if page := getNetzlaufwerk(cfg, "gast", "gaeste", "").Body.String(); !strings.Contains(page, "nicht für das Netzlaufwerk freigegeben") || strings.Contains(page, "<form") {
		t.Errorf("a guest gets no form:\n%s", page)
	}
	admin := getNetzlaufwerk(cfg, "core", "admins,family", "").Body.String()
	if !strings.Contains(admin, "Passwörter der Familie") || !strings.Contains(admin, "<option>ben</option>") || !strings.Contains(admin, "Entfernen") {
		t.Errorf("an admin sees everyone's and can reset them:\n%s", admin)
	}
	if rec := getNetzlaufwerk(StartConfig{}, "anna", "family", ""); rec.Code != 302 {
		t.Errorf("without file-share no page: %d", rec.Code)
	}
}

func TestNetzlaufwerkSetsYourOwnAndOnlyAnAdminSomeoneElses(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	fs, cfg := netzlaufwerk(t, true)
	h := Netzlaufwerk(cfg)
	pw := "Unser Haus 2026!"
	form := url.Values{"tun": {"setzen"}, "wer": {"anna"}, "passwort": {pw}, "nochmal": {pw}}
	rec := post(t, h, "/netzlaufwerk", form, "anna", nil)
	if rec.Code != 303 || rec.Header().Get("Location") != "/netzlaufwerk?gespeichert=anna" {
		t.Fatalf("saved, and back to the page: %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if l := <-fs.Lines; l != "set anna "+base64.StdEncoding.EncodeToString([]byte(pw)) {
		t.Fatalf("file-share read %q", l)
	}
	if page := getNetzlaufwerk(cfg, "anna", "family", "?gespeichert=anna").Body.String(); !strings.Contains(page, "Gespeichert") || !strings.Contains(page, "Passwort gesetzt am") {
		t.Errorf("the page says so:\n%s", page)
	}
	// someone else's: only an admin; from another site: nobody
	ben := url.Values{"tun": {"setzen"}, "wer": {"ben"}, "passwort": {pw}, "nochmal": {pw}}
	if rec := post(t, h, "/netzlaufwerk", ben, "anna", nil); rec.Code != 403 {
		t.Errorf("anna sets ben's: %d", rec.Code)
	}
	if rec := post(t, h, "/netzlaufwerk", form, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != 403 {
		t.Errorf("from another site: %d", rec.Code)
	}
	fs.NoLine(t)
	if rec := post(t, h, "/netzlaufwerk", ben, "core", map[string]string{"Remote-Groups": "admins"}); rec.Code != 303 || <-fs.Lines == "" {
		t.Errorf("an admin sets ben's: %d", rec.Code)
	}
	if rec := post(t, h, "/netzlaufwerk", url.Values{"tun": {"entfernen"}, "wer": {"ben"}}, "core", map[string]string{"Remote-Groups": "admins"}); rec.Code != 303 || <-fs.Lines != "clear ben" {
		t.Errorf("an admin removes ben's: %d", rec.Code)
	}
	// two that differ, or too short: said, nothing handed over
	for _, f := range []url.Values{{"tun": {"setzen"}, "wer": {"anna"}, "passwort": {pw}, "nochmal": {pw + "x"}}, {"tun": {"setzen"}, "wer": {"anna"}, "passwort": {"kurz"}, "nochmal": {"kurz"}}} {
		rec := post(t, h, "/netzlaufwerk", f, "anna", nil)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), "class=achtung") {
			t.Errorf("refused with a word: %d\n%s", rec.Code, rec.Body.String())
		}
	}
	fs.NoLine(t)
	if strings.Contains(logs.String(), pw) || strings.Contains(logs.String(), base64.StdEncoding.EncodeToString([]byte(pw))) {
		t.Fatalf("the password is in the log: %s", logs.String())
	}
}

func TestNetzlaufwerkSaysWhenSambaDoesNotAnswer(t *testing.T) {
	samba.Wait = 300 * time.Millisecond
	_, cfg := netzlaufwerk(t, false)
	pw := "lang genug!"
	rec := post(t, Netzlaufwerk(cfg), "/netzlaufwerk", url.Values{"tun": {"setzen"}, "wer": {"anna"}, "passwort": {pw}, "nochmal": {pw}}, "anna", nil)
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "nicht bestätigt") || strings.Contains(rec.Body.String(), pw) {
		t.Fatalf("%d\n%s", rec.Code, rec.Body.String())
	}
}

func TestStartLeadsToTheNetzlaufwerk(t *testing.T) {
	_, cfg := netzlaufwerk(t, true)
	for user, want := range map[string]string{"anna": "Noch kein Passwort fürs Netzlaufwerk", "ben": "Passwort fürs Netzlaufwerk ändern"} {
		req := httptest.NewRequest("GET", "/start", nil)
		req.Header.Set("Remote-User", user)
		rec := httptest.NewRecorder()
		Start(cfg).ServeHTTP(rec, req)
		if !strings.Contains(rec.Body.String(), want) || !strings.Contains(rec.Body.String(), "href=/netzlaufwerk") {
			t.Errorf("%s: Start lacks %q", user, want)
		}
	}
}
