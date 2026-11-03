package napi

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// haus#209: a new phone of the same person gets the newest Signal and
// WhatsApp backup back from archiv-eingang/<person>/letzte/, resumably;
// the media only through the person's open archive.

// newLetzte is Haus with the inbox; anna1 is Anna's old phone, anna2 her
// new one, ben1 Ben's.
func newLetzte(t *testing.T) *archiv {
	t.Helper()
	free(t, 100<<30)
	a := &archiv{t: t, dir: t.TempDir(), data: t.TempDir()}
	a.s = NewSicherung(a.dir, a.data)
	people := map[string]string{"anna1": "anna", "anna2": "anna", "ben1": "ben"}
	a.h = Handler(Config{Validate: StaticValidator([]string{"anna1", "anna2", "ben1"}), Sicherung: a.s,
		Whoami: func(b string) (string, bool) { p, ok := people[b]; return p, ok }})
	return a
}

// lege puts a file into person's letzte/<rel>.
func (a *archiv) lege(person, rel string, b []byte) string {
	a.t.Helper()
	p := filepath.Join(a.dir, person, letzte, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		a.t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		a.t.Fatal(err)
	}
	return p
}

// ageGestalt is a file shaped like age's: header, nonce, 64 KiB chunks
// with a 16-byte tag each. Haus reads only its header, never decrypts.
func ageGestalt(klar int) []byte {
	var b bytes.Buffer
	b.WriteString("age-encryption.org/v1\n-> X25519 c2Fsei1zdGFuemE\nYWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo\n--- bWFj\n")
	b.Write(make([]byte, 16))
	for rest := klar; ; rest -= 64 << 10 {
		n := min(rest, 64<<10)
		b.Write(make([]byte, n+16))
		if rest <= 64<<10 {
			break
		}
	}
	return b.Bytes()
}

// fakeArchiv is a person's archive's /intern: the secret checked, 423
// while closed, the plaintext with Range while open.
type fakeArchiv struct {
	*httptest.Server
	offen bool
	media map[string][]byte
	saw   []string
}

func newFakeArchiv(t *testing.T, secret string) *fakeArchiv {
	f := &fakeArchiv{offen: true, media: map[string][]byte{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.saw = append(f.saw, r.URL.Path)
		if r.Header.Get("X-Archiv-Haus") != secret {
			http.Error(w, `{"fehler":"nein"}`, 401)
			return
		}
		if !f.offen {
			http.Error(w, `{"fehler":"zu"}`, http.StatusLocked)
			return
		}
		b, ok := f.media[strings.TrimPrefix(r.URL.Path, "/intern/whatsapp/medien/")]
		if !ok {
			http.Error(w, `{"fehler":"weg"}`, 404)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		http.ServeContent(w, r, "", time.Unix(1700000000, 0), bytes.NewReader(b))
	}))
	t.Cleanup(f.Close)
	return f
}

func TestLetzteListsWhatANewPhoneGetsBack(t *testing.T) {
	a := newLetzte(t)
	a.lege("anna", "signal/signal-2026-11-02-03-00-00.backup", []byte("SIGNAL"))
	a.lege("anna", "whatsapp/Databases/msgstore.db.crypt15", []byte("WA-DB"))
	a.lege("anna", "whatsapp/Databases/.msgstore.db.crypt15.x7", []byte("halb")) // the archive's temporary name
	a.lege("anna", "whatsapp/Media/WhatsApp Images/IMG-1.jpg.age", ageGestalt(200_000))
	a.lege("anna", "whatsapp/Media/WhatsApp Voice Notes/PTT-1.opus.age", ageGestalt(0))
	a.lege("anna", "whatsapp/Media/klar.jpg", []byte("never plaintext"))
	when := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(a.dir, "anna", letzte, "signal", "signal-2026-11-02-03-00-00.backup"), when, when)

	rec, out := a.do("GET", "/napi/sicherung/letzte", "anna2", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("→ %d %s", rec.Code, rec.Body)
	}
	q := out["quellen"].(map[string]any)
	sig := q["signal"].([]any)
	if len(sig) != 1 || sig[0].(map[string]any)["pfad"] != "signal-2026-11-02-03-00-00.backup" ||
		sig[0].(map[string]any)["groesse"] != 6.0 || sig[0].(map[string]any)["geaendert"] != float64(when.UnixMilli()) {
		t.Errorf("signal: %v", sig)
	}
	var wa []string
	for _, e := range q["whatsapp"].([]any) {
		m := e.(map[string]any)
		wa = append(wa, m["pfad"].(string)+"="+strconv.FormatFloat(m["groesse"].(float64), 'f', -1, 64))
	}
	want := []string{"Databases/msgstore.db.crypt15=5", "Media/WhatsApp Images/IMG-1.jpg=200000", "Media/WhatsApp Voice Notes/PTT-1.opus=0"}
	if strings.Join(wa, "|") != strings.Join(want, "|") {
		t.Errorf("whatsapp:\n got %v\nwant %v", wa, want)
	}
	// nothing kept yet: empty lists, not null
	rec, _ = a.do("GET", "/napi/sicherung/letzte", "ben1", nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"signal":[]`) || !strings.Contains(rec.Body.String(), `"whatsapp":[]`) {
		t.Errorf("ben, nothing kept → %d %s", rec.Code, rec.Body)
	}
}

// A 3 GB Signal backup comes in pieces, and after a cut from where it
// stopped; a replaced backup is not mixed into a resumed one.
func TestLetzteThreeGigabytesResumeAfterACut(t *testing.T) {
	a := newLetzte(t)
	const groesse int64 = 3 << 30
	p := a.lege("anna", "signal/signal-2026-11-02-03-00-00.backup", nil)
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteAt([]byte("ANFANG"), 0)
	f.WriteAt([]byte("MITTE"), 1<<20)
	f.WriteAt([]byte("ENDE!!"), groesse-6)
	f.Truncate(groesse)
	f.Close()
	srv := httptest.NewServer(a.h)
	defer srv.Close()
	get := func(bearer, rng, ifRange string) *http.Response {
		req, _ := http.NewRequest("GET", srv.URL+"/napi/sicherung/letzte/signal/signal-2026-11-02-03-00-00.backup", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		if ifRange != "" {
			req.Header.Set("If-Range", ifRange)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	// the whole file is announced; the phone reads 1 MB, then the line drops
	resp := get("anna2", "", "")
	if resp.StatusCode != 200 || resp.ContentLength != groesse || resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("whole → %d %d %q", resp.StatusCode, resp.ContentLength, resp.Header.Get("Accept-Ranges"))
	}
	etag := resp.Header.Get("ETag")
	first := make([]byte, 1<<20)
	if _, err := io.ReadFull(resp.Body, first); err != nil || string(first[:6]) != "ANFANG" {
		t.Fatalf("first MB: %v %q", err, first[:6])
	}
	resp.Body.Close()
	// resumed where it stopped, the same file
	resp = get("anna2", "bytes=1048576-1048580", etag)
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || string(b) != "MITTE" || resp.Header.Get("Content-Range") != "bytes 1048576-1048580/3221225472" {
		t.Fatalf("resume → %d %q %q", resp.StatusCode, b, resp.Header.Get("Content-Range"))
	}
	resp = get("anna2", "bytes=3221225466-", etag)
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || string(b) != "ENDE!!" {
		t.Fatalf("the last piece → %d %q", resp.StatusCode, b)
	}
	// the next night's backup replaced it: an old If-Range gets the whole new file, not a mix
	later := time.Now().Add(time.Hour)
	os.Chtimes(p, later, later)
	resp = get("anna2", "bytes=1048576-", etag)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength != groesse {
		t.Fatalf("replaced, old ETag → %d %d, want the whole file", resp.StatusCode, resp.ContentLength)
	}
}

// Another person's device asks its own letzte/: Anna's backup is 404 to
// Ben, and paths out of letzte/ are 404 too.
func TestLetzteIsOnlyThePersonsOwn(t *testing.T) {
	a := newLetzte(t)
	a.lege("anna", "signal/signal-1.backup", []byte("SIGNAL"))
	a.lege("anna", "whatsapp/Databases/msgstore.db.crypt15", []byte("WA-DB"))
	os.WriteFile(filepath.Join(a.dir, "anna", ".briefkasten.pub"), []byte("age1x"), 0o644)
	if rec, _ := a.do("GET", "/napi/sicherung/letzte/signal/signal-1.backup", "anna2", nil, nil); rec.Code != 200 || rec.Body.String() != "SIGNAL" {
		t.Fatalf("anna's new phone → %d %q", rec.Code, rec.Body)
	}
	for _, p := range []string{"signal/signal-1.backup", "whatsapp/Databases/msgstore.db.crypt15"} {
		if rec, out := a.do("GET", "/napi/sicherung/letzte/"+p, "ben1", nil, nil); rec.Code != 404 || out["fehler"] != saysLetzteNichtDa {
			t.Errorf("ben asks anna's %s → %d %v", p, rec.Code, out)
		}
	}
	for _, p := range []string{"signal/../../.briefkasten.pub", "signal/%2e%2e/%2e%2e/.briefkasten.pub", "sms/x", "whatsapp/Databases/.msgstore.db.crypt15.x7", "signal/"} {
		if rec, _ := a.do("GET", "/napi/sicherung/letzte/"+p, "anna2", nil, nil); rec.Code == 200 {
			t.Errorf("%s → 200 %q", p, rec.Body)
		}
	}
	if rec, _ := a.do("GET", "/napi/sicherung/letzte/signal/signal-1.backup", "fremd", nil, nil); rec.Code != 401 {
		t.Errorf("no device → %d", rec.Code)
	}
}

// The media come from the person's open archive, Range passed on; while
// it is closed they are 423 with the sentence, Signal and the WhatsApp
// database still come.
func TestLetzteMediaOnlyWhileTheArchiveIsOpen(t *testing.T) {
	a := newLetzte(t)
	a.lege("anna", "signal/signal-1.backup", []byte("SIGNAL"))
	a.lege("anna", "whatsapp/Databases/msgstore.db.crypt15", []byte("WA-DB"))
	a.lege("anna", "whatsapp/Media/WhatsApp Images/IMG 1.jpg.age", ageGestalt(11))
	arch := newFakeArchiv(t, "anna-geheim")
	arch.media["WhatsApp Images/IMG 1.jpg"] = []byte("BILD-INHALT")
	medium := "/napi/sicherung/letzte/whatsapp/Media/WhatsApp%20Images/IMG%201.jpg"

	// no way into Anna's archive configured: a sentence, not a hang
	if rec, out := a.do("GET", medium, "anna2", nil, nil); rec.Code != 503 || out["fehler"] != saysMedienNichtHier {
		t.Fatalf("no archive configured → %d %v", rec.Code, out)
	}
	a.s.SetArchive(map[string]ArchivIntern{"anna": {URL: arch.URL, Secret: "anna-geheim"}, "ben": {URL: arch.URL, Secret: "falsch"}})

	rec, _ := a.do("GET", medium, "anna2", nil, nil)
	if rec.Code != 200 || rec.Body.String() != "BILD-INHALT" || rec.Header().Get("Content-Type") != "image/jpeg" || rec.Header().Get("Content-Length") != "11" {
		t.Fatalf("open → %d %q %v", rec.Code, rec.Body, rec.Header())
	}
	if got := arch.saw[len(arch.saw)-1]; got != "/intern/whatsapp/medien/WhatsApp Images/IMG 1.jpg" {
		t.Errorf("asked the archive for %q", got)
	}
	rec, _ = a.do("GET", medium, "anna2", map[string]string{"Range": "bytes=5-"}, nil)
	if rec.Code != 206 || rec.Body.String() != "INHALT" || rec.Header().Get("Content-Range") != "bytes 5-10/11" {
		t.Fatalf("resumed medium → %d %q %q", rec.Code, rec.Body, rec.Header().Get("Content-Range"))
	}
	if rec, _ := a.do("GET", "/napi/sicherung/letzte/whatsapp/Media/weg.jpg", "anna2", nil, nil); rec.Code != 404 {
		t.Errorf("a medium the archive has not → %d", rec.Code)
	}

	arch.offen = false
	if rec, out := a.do("GET", medium, "anna2", nil, nil); rec.Code != http.StatusLocked || out["fehler"] != saysMedienZu {
		t.Fatalf("closed → %d %v", rec.Code, out)
	}
	for p, want := range map[string]string{"signal/signal-1.backup": "SIGNAL", "whatsapp/Databases/msgstore.db.crypt15": "WA-DB"} {
		if rec, _ := a.do("GET", "/napi/sicherung/letzte/"+p, "anna2", nil, nil); rec.Code != 200 || rec.Body.String() != want {
			t.Errorf("closed, %s → %d %q", p, rec.Code, rec.Body)
		}
	}
	// an archive refusing Haus's secret is the admin's matter, said so
	arch.offen = true
	if rec, out := a.do("GET", medium, "ben1", nil, nil); rec.Code != http.StatusBadGateway || out["fehler"] != saysArchivSchweigt {
		t.Errorf("wrong secret → %d %v", rec.Code, out)
	}
	// an archive that is down
	arch.Close()
	if rec, out := a.do("GET", medium, "anna2", nil, nil); rec.Code != 503 || out["fehler"] != saysArchivSchweigt {
		t.Errorf("archive down → %d %v", rec.Code, out)
	}
}

func TestArchivInternFromEnv(t *testing.T) {
	env := map[string]string{
		"HAUS_ARCHIV_INTERN":             "anna=http://host.containers.internal:18540/ max-mueller=http://host.containers.internal:18541 ben=ftp://x kaputt ohne=http://h:1",
		"HAUS_ARCHIV_SECRET_ANNA":        "a",
		"HAUS_ARCHIV_SECRET_MAX_MUELLER": "m",
		"HAUS_ARCHIV_SECRET_BEN":         "b",
	}
	got := ArchivInternFromEnv(func(k string) string { return env[k] })
	if len(got) != 2 || got["anna"] != (ArchivIntern{"http://host.containers.internal:18540", "a"}) || got["max-mueller"] != (ArchivIntern{"http://host.containers.internal:18541", "m"}) {
		t.Errorf("%v", got)
	}
}
