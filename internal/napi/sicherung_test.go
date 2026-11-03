package napi

import (
	"bytes"
	"encoding/json"
	"errors"
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

// haus#207, laib ADR 0029: the Homes app brings a person's WhatsApp, Signal
// backups into archiv-eingang/<person>/<quelle>/<pfad>, resumably;
// the person is the device's, never the request's.

type archiv struct {
	t    *testing.T
	dir  string // HOMES_ARCHIV_DIR
	data string // HOMES_DATA
	s    *Sicherung
	h    http.Handler
}

// newArchiv is Homes with the inbox; anna1 is Anna's phone, ben1 Ben's,
// waise1 a device of nobody. The disk has 100 GB free unless a test says else.
func newArchiv(t *testing.T) *archiv {
	t.Helper()
	free(t, 100<<30)
	a := &archiv{t: t, dir: t.TempDir(), data: t.TempDir()}
	a.s = NewSicherung(a.dir, a.data)
	a.h = archivHandler(a.s)
	return a
}

func archivHandler(s *Sicherung) http.Handler {
	people := map[string]string{"anna1": "anna", "ben1": "ben", "waise1": ""}
	return Handler(Config{Validate: StaticValidator([]string{"anna1", "ben1", "waise1"}), Sicherung: s,
		Whoami: func(b string) (string, bool) { p, ok := people[b]; return p, ok }})
}

// free makes the disk say it has n bytes free, for this test.
func free(t *testing.T, n uint64) {
	real := freeSpace
	t.Cleanup(func() { freeSpace = real })
	freeSpace = func(string) (uint64, error) { return n, nil }
}

func (a *archiv) do(method, path, bearer string, hdr map[string]string, body io.Reader) (*httptest.ResponseRecorder, map[string]any) {
	a.t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Authorization", "Bearer "+bearer)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

// start is POST /uploads; the id and offset of a 201.
func (a *archiv) start(bearer, quelle, pfad string, groesse int64) (string, int64) {
	a.t.Helper()
	b, _ := json.Marshal(map[string]any{"quelle": quelle, "pfad": pfad, "groesse": groesse})
	rec, out := a.do("POST", "/napi/sicherung/uploads", bearer, nil, bytes.NewReader(b))
	if rec.Code != 201 {
		a.t.Fatalf("start %s/%s → %d %s", quelle, pfad, rec.Code, rec.Body)
	}
	id, _ := out["id"].(string)
	off, _ := out["offset"].(float64)
	return id, int64(off)
}

func (a *archiv) piece(bearer, id string, at int64, body io.Reader) (*httptest.ResponseRecorder, map[string]any) {
	a.t.Helper()
	return a.do("PATCH", "/napi/sicherung/uploads/"+id, bearer,
		map[string]string{"Upload-Offset": strconv.FormatInt(at, 10), "Content-Type": "application/offset+octet-stream"}, body)
}

// head is HEAD's status and Upload-Offset.
func (a *archiv) head(bearer, id string) (int, string) {
	a.t.Helper()
	rec, _ := a.do("HEAD", "/napi/sicherung/uploads/"+id, bearer, nil, nil)
	return rec.Code, rec.Header().Get("Upload-Offset")
}

func (a *archiv) fehlend(bearer, quelle string, dateien ...datei) []string {
	a.t.Helper()
	b, _ := json.Marshal(map[string]any{"quelle": quelle, "dateien": dateien})
	rec, out := a.do("POST", "/napi/sicherung/fehlend", bearer, nil, bytes.NewReader(b))
	if rec.Code != 200 {
		a.t.Fatalf("fehlend → %d %s", rec.Code, rec.Body)
	}
	got := []string{}
	for _, p := range out["fehlend"].([]any) {
		got = append(got, p.(string))
	}
	return got
}

// cutReader gives its bytes, then the connection breaks.
type cutReader struct{ b []byte }

func (c *cutReader) Read(p []byte) (int, error) {
	if len(c.b) == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	n := copy(p, c.b)
	c.b = c.b[n:]
	return n, nil
}

// chunked hides a body's length, as a phone streaming a piece does.
type chunked struct{ r io.Reader }

func (c chunked) Read(p []byte) (int, error) { return c.r.Read(p) }

func TestSicherungResumesAfterACut(t *testing.T) {
	a := newArchiv(t)
	content := []byte("WhatsApp-Sicherung: eine verschlüsselte Datenbank")
	n := int64(len(content))
	const pfad = "Databases/msgstore.db.crypt15"
	id, off := a.start("anna1", "whatsapp", pfad, n)
	if off != 0 || len(id) != 32 {
		t.Fatalf("a new upload: id %q offset %d", id, off)
	}
	rec, out := a.piece("anna1", id, 0, bytes.NewReader(content[:10]))
	if rec.Code != 200 || out["offset"] != 10.0 || out["fertig"] != false {
		t.Fatalf("first piece → %d %v", rec.Code, out)
	}
	// the connection breaks seven bytes into the next piece: they are kept
	rec, out = a.piece("anna1", id, 10, &cutReader{content[10:17]})
	if rec.Code == 200 || out["fehler"] == nil {
		t.Fatalf("a cut piece → %d %v", rec.Code, out)
	}
	if code, at := a.head("anna1", id); code != 204 || at != "17" {
		t.Fatalf("HEAD after the cut → %d %q, want 204 17", code, at)
	}
	// the phone asks again for the same file: the same upload, where it is
	if again, off := a.start("anna1", "whatsapp", pfad, n); again != id || off != 17 {
		t.Fatalf("starting the same file again → %q %d, want %q 17", again, off, id)
	}
	if _, err := os.Stat(filepath.Join(a.dir, "anna", "whatsapp", pfad)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a half file is in the inbox: %v", err)
	}
	rec, out = a.piece("anna1", id, 17, chunked{bytes.NewReader(content[17:])})
	if rec.Code != 200 || out["offset"] != float64(n) || out["fertig"] != true {
		t.Fatalf("the rest → %d %v", rec.Code, out)
	}
	got, err := os.ReadFile(filepath.Join(a.dir, "anna", "whatsapp", pfad))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("in the inbox: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(a.dir, "anna", teil, id)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the part stayed behind: %v", err)
	}
	// the last answer got lost: the phone asks, and hears it is whole
	if code, at := a.head("anna1", id); code != 204 || at != strconv.FormatInt(n, 10) {
		t.Errorf("HEAD of a finished upload → %d %q", code, at)
	}
	if rec, out := a.piece("anna1", id, n, bytes.NewReader(nil)); rec.Code != 200 || out["fertig"] != true {
		t.Errorf("an empty last piece again → %d %v", rec.Code, out)
	}
	if rec, out := a.piece("anna1", id, 17, bytes.NewReader(content[17:])); rec.Code != 409 || out["offset"] != float64(n) {
		t.Errorf("the last piece again → %d %v, want 409 at %d", rec.Code, out, n)
	}
}

func TestSicherungWrongOffsetIs409(t *testing.T) {
	a := newArchiv(t)
	id, _ := a.start("anna1", "signal", "signal-2026-11-01-03-00-00.backup", 20)
	a.piece("anna1", id, 0, strings.NewReader("12345"))
	for _, at := range []int64{0, 4, 6, 19} {
		rec, out := a.piece("anna1", id, at, strings.NewReader("abc"))
		if rec.Code != 409 || out["offset"] != 5.0 || out["fehler"] == "" {
			t.Errorf("a piece at %d when the box has 5 → %d %v, want 409 with offset 5", at, rec.Code, out)
		}
	}
	if _, at := a.head("anna1", id); at != "5" {
		t.Fatalf("a refused piece changed the part: %s", at)
	}
	// a piece still arriving that does not let go: the next one hears where the box is
	defer func(w time.Duration) { sicherungUebernahme = w }(sicherungUebernahme)
	sicherungUebernahme = 50 * time.Millisecond
	if !a.s.claim("anna", id, nil, 0) {
		t.Fatal("claim")
	}
	rec, out := a.piece("anna1", id, 5, strings.NewReader("abc"))
	a.s.release("anna", id)
	if rec.Code != 409 || out["offset"] != 5.0 {
		t.Errorf("a piece while another is written → %d %v", rec.Code, out)
	}
	// one hanging on a dead connection is stopped, lets go, and the new piece goes on
	stopped := false
	if !a.s.claim("anna", id, func() { stopped = true; go a.s.release("anna", id) }, 0) {
		t.Fatal("claim")
	}
	if rec, out := a.piece("anna1", id, 5, strings.NewReader("abc")); rec.Code != 200 || out["offset"] != 8.0 || !stopped {
		t.Errorf("a piece after a hanging one → %d %v (stopped %v)", rec.Code, out, stopped)
	}
	if rec, out := a.do("PATCH", "/napi/sicherung/uploads/"+id, "anna1", map[string]string{"Content-Type": "application/offset+octet-stream"}, strings.NewReader("abc")); rec.Code != 400 || out["fehler"] == nil {
		t.Errorf("without Upload-Offset → %d %v", rec.Code, out)
	}
	if rec, _ := a.do("PATCH", "/napi/sicherung/uploads/"+id, "anna1", map[string]string{"Upload-Offset": "5", "Content-Type": "text/plain"}, strings.NewReader("abc")); rec.Code != 415 {
		t.Errorf("another content type → %d", rec.Code)
	}
}

func TestSicherungRefusesMoreThanTheFile(t *testing.T) {
	a := newArchiv(t)
	id, _ := a.start("anna1", "signal", "kurz.backup", 5)
	if rec, out := a.piece("anna1", id, 0, strings.NewReader("123456")); rec.Code != 400 || out["fehler"] != saysMoreThanSaid {
		t.Errorf("six bytes for five, with a length → %d %v", rec.Code, out)
	}
	if rec, out := a.piece("anna1", id, 0, chunked{strings.NewReader("123456")}); rec.Code != 400 || out["fehler"] != saysMoreThanSaid {
		t.Errorf("six bytes for five, streamed → %d %v", rec.Code, out)
	}
	if _, at := a.head("anna1", id); at != "0" {
		t.Errorf("a refused piece left %s bytes", at)
	}
	big := httptest.NewRequest("PATCH", "/napi/sicherung/uploads/"+id, strings.NewReader("x"))
	big.Header.Set("Authorization", "Bearer anna1")
	big.Header.Set("Upload-Offset", "0")
	big.Header.Set("Content-Type", "application/offset+octet-stream")
	big.ContentLength = 17 << 20
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, big)
	if rec.Code != 413 {
		t.Errorf("a 17 MB piece → %d", rec.Code)
	}
}

// A backup of several GB, declared and finished without writing it: the
// part is made sparse up to its last bytes.
func TestSicherungThreeGigabytes(t *testing.T) {
	a := newArchiv(t)
	const groesse int64 = 3 << 30
	const pfad = "signal-2026-11-01-03-00-00.backup"
	id, _ := a.start("anna1", "signal", pfad, groesse)
	if rec, out := a.piece("anna1", id, 0, bytes.NewReader(make([]byte, 1<<20))); rec.Code != 200 || out["offset"] != float64(1<<20) {
		t.Fatalf("the first MB → %d %v", rec.Code, out)
	}
	if err := os.Truncate(filepath.Join(a.dir, "anna", teil, id), groesse-6); err != nil {
		t.Fatal(err)
	}
	if code, at := a.head("anna1", id); code != 204 || at != strconv.FormatInt(groesse-6, 10) {
		t.Fatalf("HEAD → %d %s", code, at)
	}
	// the disk fills meanwhile: the rest and the margin no longer fit
	free(t, 1<<30)
	if rec, out := a.piece("anna1", id, groesse-6, strings.NewReader("ende!!")); rec.Code != 507 || out["fehler"] != saysNoRoom {
		t.Fatalf("a full disk → %d %v, want 507", rec.Code, out)
	}
	free(t, 100<<30)
	if rec, out := a.piece("anna1", id, groesse-6, strings.NewReader("ende!!")); rec.Code != 200 || out["fertig"] != true || out["offset"] != float64(groesse) {
		t.Fatalf("the last bytes → %d %v", rec.Code, out)
	}
	fi, err := os.Stat(filepath.Join(a.dir, "anna", "signal", pfad))
	if err != nil || fi.Size() != groesse {
		t.Fatalf("in the inbox: %v %v", fi, err)
	}
	if got := a.fehlend("anna1", "signal", datei{pfad, groesse}); len(got) != 0 {
		t.Errorf("fehlend after it arrived: %v", got)
	}
	rec, out := a.do("GET", "/napi/sicherung/stand", "anna1", nil, nil)
	sig := out["quellen"].(map[string]any)["signal"].(map[string]any)
	if rec.Code != 200 || sig["dateien"] != 1.0 || sig["bytes"] != float64(groesse) || sig["wartend"] != 1.0 || sig["zuletzt"] == nil || out["frei_bytes"] != float64(100<<30) {
		t.Errorf("stand → %d %v", rec.Code, out)
	}
	// a 3 GB file on a disk that cannot take it is refused before its first byte
	id2, _ := a.start("anna1", "signal", "zweite.backup", groesse)
	free(t, 2<<30)
	if rec, _ := a.piece("anna1", id2, 0, strings.NewReader("x")); rec.Code != 507 {
		t.Errorf("3 GB on 2 GB free → %d, want 507", rec.Code)
	}
	if b, _ := json.Marshal(map[string]any{"quelle": "signal", "pfad": "riesig.backup", "groesse": int64(21 << 30)}); true {
		if rec, out := a.do("POST", "/napi/sicherung/uploads", "anna1", nil, bytes.NewReader(b)); rec.Code != 400 || out["fehler"] != saysTooBig {
			t.Errorf("21 GB → %d %v", rec.Code, out)
		}
	}
}

func TestSicherungRefusesPathsOutOfTheInbox(t *testing.T) {
	a := newArchiv(t)
	for _, f := range []struct{ quelle, pfad string }{
		{"whatsapp", "../ben/whatsapp/x.jpg"}, {"whatsapp", "Media/../../ben/x.jpg"}, {"whatsapp", "/etc/passwd"},
		{"whatsapp", "Media//x.jpg"}, {"whatsapp", ".teil/0123"}, {"whatsapp", "Media/.Statuses/x.jpg"}, {"whatsapp", "Media/.nomedia"},
		{"whatsapp", ""}, {"whatsapp", "x/"}, {"whatsapp", "a\x00b"}, {"whatsapp", strings.Repeat("a", 513)},
		{"../ben", "x.jpg"}, {"telegram", "x.jpg"}, {"", "x.jpg"},
	} {
		b, _ := json.Marshal(map[string]any{"quelle": f.quelle, "pfad": f.pfad, "groesse": 3})
		if rec, out := a.do("POST", "/napi/sicherung/uploads", "anna1", nil, bytes.NewReader(b)); rec.Code != 400 || out["fehler"] == nil {
			t.Errorf("%q/%q → %d %v, want 400", f.quelle, f.pfad, rec.Code, out)
		}
		b, _ = json.Marshal(map[string]any{"quelle": f.quelle, "dateien": []datei{{f.pfad, 3}}})
		if rec, _ := a.do("POST", "/napi/sicherung/fehlend", "anna1", nil, bytes.NewReader(b)); rec.Code != 400 {
			t.Errorf("fehlend %q/%q → %d, want 400", f.quelle, f.pfad, rec.Code)
		}
	}
	// a link inside the inbox that points out of it: os.Root does not follow
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(a.dir, "anna", "whatsapp"), 0o755)
	if err := os.Symlink(outside, filepath.Join(a.dir, "anna", "whatsapp", "Media")); err != nil {
		t.Fatal(err)
	}
	id, _ := a.start("anna1", "whatsapp", "Media/x.jpg", 3)
	if rec, _ := a.piece("anna1", id, 0, strings.NewReader("abc")); rec.Code != 500 {
		t.Errorf("placing through a link out → %d, want 500", rec.Code)
	}
	if es, _ := os.ReadDir(outside); len(es) != 0 {
		t.Fatalf("written outside the inbox: %v", es)
	}
}

func TestSicherungIsThePersonOfTheDevice(t *testing.T) {
	a := newArchiv(t)
	id, _ := a.start("anna1", "whatsapp", "Databases/msgstore.db.crypt15", 6)
	a.piece("anna1", id, 0, strings.NewReader("abc"))
	if code, _ := a.head("ben1", id); code != 404 {
		t.Errorf("Ben's HEAD of Anna's upload → %d, want 404", code)
	}
	if rec, _ := a.piece("ben1", id, 3, strings.NewReader("def")); rec.Code != 404 {
		t.Errorf("Ben's piece for Anna's upload → %d, want 404", rec.Code)
	}
	if code, at := a.head("anna1", id); code != 204 || at != "3" {
		t.Errorf("Anna's upload after Ben tried → %d %s", code, at)
	}
	for _, id := range []string{"unbekannt", "..%2Fben", strings.Repeat("0", 32), strings.ToUpper(id)} {
		if code, _ := a.head("anna1", id); code != 404 {
			t.Errorf("HEAD %q → %d, want 404", id, code)
		}
	}
	// the same file from Ben's phone is Ben's, in Ben's folder
	bid, off := a.start("ben1", "whatsapp", "Databases/msgstore.db.crypt15", 6)
	if off != 0 {
		t.Errorf("Ben's upload starts at %d", off)
	}
	a.piece("ben1", bid, 0, strings.NewReader("BENBEN"))
	if b, err := os.ReadFile(filepath.Join(a.dir, "ben", "whatsapp", "Databases", "msgstore.db.crypt15")); err != nil || string(b) != "BENBEN" {
		t.Errorf("Ben's file: %q %v", b, err)
	}
	if got := a.fehlend("anna1", "whatsapp", datei{"Databases/msgstore.db.crypt15", 6}); len(got) != 1 {
		t.Errorf("Ben's arrival counts for Anna: %v", got)
	}
	// a device of nobody writes nowhere
	for _, q := range []struct{ method, path string }{{"GET", "/napi/sicherung/stand"}, {"POST", "/napi/sicherung/uploads"}, {"POST", "/napi/sicherung/fehlend"}} {
		if rec, out := a.do(q.method, q.path, "waise1", nil, strings.NewReader(`{"quelle":"whatsapp","pfad":"x","groesse":1}`)); rec.Code != 403 || out["fehler"] != saysNoPerson {
			t.Errorf("%s %s from a device of nobody → %d %v", q.method, q.path, rec.Code, out)
		}
	}
}

// The importer deletes what it took: what arrived is Homes' ledger's to say.
func TestSicherungFehlendFromTheLedger(t *testing.T) {
	a := newArchiv(t)
	put := func(pfad, content string) {
		id, _ := a.start("anna1", "whatsapp", pfad, int64(len(content)))
		if rec, out := a.piece("anna1", id, 0, strings.NewReader(content)); out["fertig"] != true {
			t.Fatalf("%s → %d %v", pfad, rec.Code, out)
		}
	}
	put("Media/WhatsApp Images/IMG-20261101-WA0001.jpg", "bild")
	put("Databases/msgstore.db.crypt15", "gestern")
	put("Databases/msgstore.db.crypt15", "heute!!!") // the daily backup keeps its name: replaced
	if b, _ := os.ReadFile(filepath.Join(a.dir, "anna", "whatsapp", "Databases", "msgstore.db.crypt15")); string(b) != "heute!!!" {
		t.Errorf("the daily backup was not replaced: %q", b)
	}
	os.RemoveAll(filepath.Join(a.dir, "anna", "whatsapp")) // the archive imported it
	ask := []datei{
		{"Media/WhatsApp Images/IMG-20261101-WA0001.jpg", 4}, // arrived
		{"Media/WhatsApp Images/IMG-20261101-WA0002.jpg", 4}, // never
		{"Databases/msgstore.db.crypt15", 7},                 // yesterday's size: today's arrived
		{"Databases/msgstore.db.crypt15", 8},
	}
	want := "Media/WhatsApp Images/IMG-20261101-WA0002.jpg,Databases/msgstore.db.crypt15"
	if got := strings.Join(a.fehlend("anna1", "whatsapp", ask...), ","); got != want {
		t.Errorf("fehlend = %s, want %s", got, want)
	}
	if got := a.fehlend("anna1", "signal", ask[0]); len(got) != 1 {
		t.Errorf("a WhatsApp file counts for Signal: %v", got)
	}
	// a restarted Homes reads the ledger from its data folder
	again := &archiv{t: t, dir: a.dir, data: a.data, s: NewSicherung(a.dir, a.data)}
	again.h = archivHandler(again.s)
	if got := strings.Join(again.fehlend("anna1", "whatsapp", ask...), ","); got != want {
		t.Errorf("after a restart fehlend = %s, want %s", got, want)
	}
	st, err := again.s.Stand("anna")
	if wa := st.Quellen["whatsapp"]; err != nil || wa.Dateien != 2 || wa.Bytes != 12 || wa.Wartend != 0 || wa.Zuletzt == nil {
		t.Errorf("stand after the import: %+v %v", st, err)
	}
	if _, ok := st.Quellen["discord"]; ok {
		t.Errorf("Discord is gone (owner, 2026-11-03): %+v", st.Quellen)
	}
	if _, err := os.Stat(filepath.Join(a.data, "sicherung", "anna", "whatsapp.jsonl")); err != nil {
		t.Errorf("the ledger is not in HOMES_DATA: %v", err)
	}
	many := make([]datei, sicherungMaxListe+1)
	for i := range many {
		many[i] = datei{"f" + strconv.Itoa(i), 1}
	}
	b, _ := json.Marshal(map[string]any{"quelle": "whatsapp", "dateien": many})
	if rec, out := a.do("POST", "/napi/sicherung/fehlend", "anna1", nil, bytes.NewReader(b)); rec.Code != 400 || out["fehler"] != saysTooMany {
		t.Errorf("5001 files → %d %v", rec.Code, out)
	}
}

func TestSicherungEmptyFileArrivesAtOnce(t *testing.T) {
	a := newArchiv(t)
	if _, off := a.start("anna1", "signal", "leer.txt", 0); off != 0 {
		t.Fatalf("offset %d", off)
	}
	if fi, err := os.Stat(filepath.Join(a.dir, "anna", "signal", "leer.txt")); err != nil || fi.Size() != 0 {
		t.Fatalf("an empty file: %v %v", fi, err)
	}
}

func TestSicherungWithoutArchivDirIs503(t *testing.T) {
	routes := []struct{ method, path, body string }{
		{"GET", "/napi/sicherung/stand", ""}, {"POST", "/napi/sicherung/fehlend", `{"quelle":"whatsapp","dateien":[]}`},
		{"POST", "/napi/sicherung/uploads", `{"quelle":"whatsapp","pfad":"x","groesse":1}`},
		{"HEAD", "/napi/sicherung/uploads/" + strings.Repeat("0", 32), ""}, {"PATCH", "/napi/sicherung/uploads/" + strings.Repeat("0", 32), "x"},
	}
	for _, c := range []struct {
		s   *Sicherung
		say string
	}{{nil, saysArchivNotSet}, {NewSicherung(filepath.Join(t.TempDir(), "fehlt"), t.TempDir()), saysArchivMissing}} {
		a := &archiv{t: t, h: archivHandler(c.s)}
		for _, q := range routes {
			rec, out := a.do(q.method, q.path, "anna1", map[string]string{"Upload-Offset": "0", "Content-Type": "application/offset+octet-stream"}, strings.NewReader(q.body))
			if rec.Code != 503 || (q.method != "HEAD" && out["fehler"] != c.say) {
				t.Errorf("%s %s → %d %v, want 503 %q", q.method, q.path, rec.Code, out, c.say)
			}
		}
	}
	if rec, out := (&archiv{t: t, h: archivHandler(nil)}).do("GET", "/napi/sicherung/stand", "anna1", nil, nil); out["fehler"] != "Das Archiv ist auf dieser Box nicht eingerichtet." {
		t.Errorf("the contract's sentence: %d %v", rec.Code, out)
	}
	rec, _ := (&archiv{t: t, h: archivHandler(nil)}).do("GET", "/napi/sicherung/stand", "fremd", nil, nil)
	if rec.Code != 401 {
		t.Errorf("a stranger → %d, want 401 before 503", rec.Code)
	}
}

func TestSicherungGivesUpOldParts(t *testing.T) {
	a := newArchiv(t)
	old, _ := a.start("anna1", "signal", "alt.backup", 10)
	a.piece("anna1", old, 0, strings.NewReader("abc"))
	fresh, _ := a.start("anna1", "signal", "neu.backup", 10)
	week := time.Now().Add(-8 * 24 * time.Hour)
	for _, n := range []string{old, old + ".json"} {
		os.Chtimes(filepath.Join(a.dir, "anna", teil, n), week, week)
	}
	if err := a.s.Aufraeumen(); err != nil {
		t.Fatal(err)
	}
	if code, _ := a.head("anna1", old); code != 404 {
		t.Errorf("a part untouched for 8 days → HEAD %d, want 404", code)
	}
	if es, _ := os.ReadDir(filepath.Join(a.dir, "anna", teil)); len(es) != 2 {
		t.Errorf("left in .teil: %v", es)
	}
	if code, _ := a.head("anna1", fresh); code != 204 {
		t.Errorf("a fresh part was given up: %d", code)
	}
}

// Over a real connection (deadlines need the server's writer): a phone
// whose network went mid-piece leaves what it sent, and when it comes back
// its next piece takes over from the one still hanging on the box.
func TestSicherungResumesOverARealConnection(t *testing.T) {
	a := newArchiv(t)
	srv := httptest.NewServer(a.h)
	defer srv.Close()
	id, _ := a.start("anna1", "signal", "netz.backup", 8)
	pr, pw := io.Pipe()
	req, _ := http.NewRequest("PATCH", srv.URL+"/napi/sicherung/uploads/"+id, pr)
	req.Header.Set("Authorization", "Bearer anna1")
	req.Header.Set("Upload-Offset", "0")
	req.Header.Set("Content-Type", "application/offset+octet-stream")
	// the phone's network goes after three bytes; to the box the
	// connection still looks open, the piece still arriving
	defer pw.CloseWithError(errors.New("das Netz ist weg"))
	go func() {
		if resp, err := srv.Client().Do(req); err == nil {
			resp.Body.Close()
		}
	}()
	pw.Write([]byte("abc"))
	at := ""
	for range 50 {
		if _, at = a.head("anna1", id); at == "3" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if at != "3" {
		t.Fatalf("after the drop the box has %q, want 3", at)
	}
	req, _ = http.NewRequest("PATCH", srv.URL+"/napi/sicherung/uploads/"+id, strings.NewReader("defgh"))
	req.Header.Set("Authorization", "Bearer anna1")
	req.Header.Set("Upload-Offset", "3")
	req.Header.Set("Content-Type", "application/offset+octet-stream")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 || out["fertig"] != true {
		t.Fatalf("the rest → %d %v", resp.StatusCode, out)
	}
	if b, _ := os.ReadFile(filepath.Join(a.dir, "anna", "signal", "netz.backup")); string(b) != "abcdefgh" {
		t.Fatalf("in the inbox: %q", b)
	}
}

// WhatsApp's msgstore.db.crypt15 keeps its name every day and can keep its
// size: with its mtime, today's file is a new upload, not yesterday's part
// resumed or yesterday's finished upload answered as whole.
func TestSicherungTodaysFileIsNotYesterdays(t *testing.T) {
	a := newArchiv(t)
	const pfad = "Databases/msgstore.db.crypt15"
	start := func(geaendert int64) (string, int64) {
		b, _ := json.Marshal(map[string]any{"quelle": "whatsapp", "pfad": pfad, "groesse": 8, "geaendert": geaendert})
		rec, out := a.do("POST", "/napi/sicherung/uploads", "anna1", nil, bytes.NewReader(b))
		if rec.Code != 201 {
			t.Fatalf("start → %d %s", rec.Code, rec.Body)
		}
		off, _ := out["offset"].(float64)
		return out["id"].(string), int64(off)
	}
	gestern, _ := start(1_793_500_000_000)
	if rec, out := a.piece("anna1", gestern, 0, bytes.NewReader([]byte("gestern!"))); rec.Code != 200 || out["fertig"] != true {
		t.Fatalf("yesterday's file → %d %v", rec.Code, out)
	}
	heute, off := start(1_793_586_400_000)
	if heute == gestern || off != 0 {
		t.Fatalf("today's file got %q at %d, yesterday's was %q: want a new upload at 0", heute, off, gestern)
	}
	if rec, out := a.piece("anna1", heute, 0, bytes.NewReader([]byte("heute!!!"))); rec.Code != 200 || out["fertig"] != true {
		t.Fatalf("today's file → %d %v", rec.Code, out)
	}
	if got, _ := os.ReadFile(filepath.Join(a.dir, "anna", "whatsapp", pfad)); string(got) != "heute!!!" {
		t.Errorf("in the inbox: %q, want today's file", got)
	}
	// yesterday's, not imported yet, may hold a chat deleted since: kept beside, not replaced
	beside, _ := filepath.Glob(filepath.Join(a.dir, "anna", "whatsapp", pfad+".*"))
	if len(beside) != 1 {
		t.Fatalf("beside today's file: %v, want yesterday's", beside)
	}
	if got, _ := os.ReadFile(beside[0]); string(got) != "gestern!" {
		t.Errorf("%s holds %q, want yesterday's file", beside[0], got)
	}
	if st, err := os.Stat(filepath.Join(a.dir, "anna", "whatsapp", pfad)); err != nil || st.ModTime().UnixMilli() != 1_793_586_400_000 {
		t.Errorf("today's file carries mtime %v (%v), want the phone's", st.ModTime(), err)
	}
}

// The phone encrypts its SMS and call log to the person's archive: GET
// /schluessel is the age recipient the archive left in the person's inbox,
// 404 in words before it did, and never another person's.
func TestSicherungSchluessel(t *testing.T) {
	a := newArchiv(t)
	rec, out := a.do("GET", "/napi/sicherung/schluessel", "anna1", nil, nil)
	if rec.Code != 404 || out["fehler"] != saysNoKey {
		t.Fatalf("no inbox yet → %d %v", rec.Code, out)
	}
	const key = "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"
	os.MkdirAll(filepath.Join(a.dir, "anna"), 0o755)
	rec, out = a.do("GET", "/napi/sicherung/schluessel", "anna1", nil, nil)
	if rec.Code != 404 || out["fehler"] != saysNoKey {
		t.Fatalf("no key yet → %d %v", rec.Code, out)
	}
	os.WriteFile(filepath.Join(a.dir, "anna", ".briefkasten.pub"), []byte(key+"\n"), 0o644)
	rec, out = a.do("GET", "/napi/sicherung/schluessel", "anna1", nil, nil)
	if rec.Code != 200 || out["empfaenger"] != key {
		t.Fatalf("→ %d %v", rec.Code, out)
	}
	if rec, out = a.do("GET", "/napi/sicherung/schluessel", "ben1", nil, nil); rec.Code != 404 {
		t.Fatalf("Ben got Anna's key: %d %v", rec.Code, out)
	}
	if rec, _ = a.do("GET", "/napi/sicherung/schluessel", "waise1", nil, nil); rec.Code != 403 {
		t.Fatalf("a device of nobody → %d", rec.Code)
	}
	for _, bad := range []string{key + " " + key, "ssh-ed25519 AAAA", "AGE1QL3Z", ""} {
		os.WriteFile(filepath.Join(a.dir, "anna", ".briefkasten.pub"), []byte(bad), 0o644)
		if rec, out = a.do("GET", "/napi/sicherung/schluessel", "anna1", nil, nil); rec.Code != 500 || out["fehler"] != saysBadKey {
			t.Errorf("%q → %d %v", bad, rec.Code, out)
		}
	}
	// a link out of the inbox is not followed (os.Root)
	outside := filepath.Join(t.TempDir(), "pub")
	os.WriteFile(outside, []byte(key), 0o644)
	os.Remove(filepath.Join(a.dir, "anna", ".briefkasten.pub"))
	os.Symlink(outside, filepath.Join(a.dir, "anna", ".briefkasten.pub"))
	if rec, _ = a.do("GET", "/napi/sicherung/schluessel", "anna1", nil, nil); rec.Code == 200 {
		t.Fatalf("a key from outside the inbox was served")
	}
}

// sms is a source like the others: the app's encrypted SMS and call log
// land in <person>/sms/ as the bytes they are.
func TestSicherungTakesSMS(t *testing.T) {
	a := newArchiv(t)
	content := []byte("age-encryption.org/v1\n-> X25519 …")
	for _, pfad := range []string{"sms-20261102T120000Z.xml.age", "anrufe-20261102T120000Z.xml.age"} {
		id, _ := a.start("anna1", "sms", pfad, int64(len(content)))
		if rec, out := a.piece("anna1", id, 0, bytes.NewReader(content)); rec.Code != 200 || out["fertig"] != true {
			t.Fatalf("%s → %d %v", pfad, rec.Code, out)
		}
		if b, err := os.ReadFile(filepath.Join(a.dir, "anna", "sms", pfad)); err != nil || !bytes.Equal(b, content) {
			t.Fatalf("%s: %q %v", pfad, b, err)
		}
	}
	st, _ := a.s.Stand("anna")
	if st.Quellen["sms"].Dateien != 2 {
		t.Fatalf("stand %+v", st.Quellen["sms"])
	}
	if say := checkFile("telegram", "x", 1); !strings.Contains(say, "whatsapp, signal und sms") {
		t.Fatalf("the sentence lists the sources wrong: %s", say)
	}
}
