package pages

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/napi"
)

// haus#68: the Dokumente page shows the ways into Paperless — a mailto to
// the inbox address, a file or the camera straight into Paperless's inbox
// (the app's folder, haus#10), "In Paperless öffnen" — each from the
// config, and what happens to a document.
func dokumente(t *testing.T) (string, StartConfig) {
	t.Helper()
	dir := t.TempDir()
	return dir, StartConfig{DocumentsMail: "dokumente@box.test", Ablage: napi.NewUploads(dir),
		PaperlessPublicURL: "https://paperless.box.test", PaperlessLANOnly: true}
}

func getDokumente(cfg StartConfig, path string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Remote-User", "anna")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	Dokumente(cfg).ServeHTTP(rec, req)
	return rec
}

func TestDokumenteShowsTheWaysIn(t *testing.T) {
	_, cfg := dokumente(t)
	home := getDokumente(cfg, "/dokumente", map[string]string{"X-Forwarded-For": "192.168.1.23"}).Body.String()
	for _, want := range []string{"<h1>Dokumente</h1>", `href="mailto:dokumente@box.test"`, "Per E-Mail ablegen", "der Betreff wird der Titel",
		`enctype=multipart/form-data`, `type=file name=file`, "application/pdf", "Dokument ablegen", "Datei wählen oder Foto machen",
		`href="https://paperless.box.test/"`, "In Paperless öffnen", "Nur zu Hause im WLAN.", "Texterkennung", House.Display + " findet es"} {
		if !strings.Contains(home, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	// from outside the house: Paperless is LAN-only, so a word instead of a dead link; filing still works
	away := getDokumente(cfg, "/dokumente", map[string]string{"X-Forwarded-For": "203.0.113.7"}).Body.String()
	if strings.Contains(away, "paperless.box.test") || !strings.Contains(away, "nur zu Hause im WLAN") || !strings.Contains(away, "Dokument ablegen") {
		t.Errorf("away from home:\n%s", away)
	}
	// Paperless reachable from anywhere: the link, no hint
	cfg.PaperlessLANOnly = false
	if open := getDokumente(cfg, "/dokumente", map[string]string{"X-Forwarded-For": "203.0.113.7"}).Body.String(); !strings.Contains(open, "In Paperless öffnen") || strings.Contains(open, "WLAN") {
		t.Errorf("not LAN-only:\n%s", open)
	}
	// only the mail: no form, no link
	only := getDokumente(StartConfig{DocumentsMail: "dokumente@box.test"}, "/dokumente", nil).Body.String()
	if strings.Contains(only, "<form") || strings.Contains(only, "In Paperless öffnen") || !strings.Contains(only, "mailto:") {
		t.Errorf("mail only:\n%s", only)
	}
	// nothing set: said, for an admin what is missing (laib#418)
	rec := getDokumente(StartConfig{}, "/dokumente", map[string]string{"Remote-Groups": "admins"})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "noch nicht eingerichtet") || !strings.Contains(rec.Body.String(), "HAUS_DOCUMENTS_MAIL") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

func upload(t *testing.T, cfg StartConfig, ctype string, data []byte, title string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("filename", title)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="scan.pdf"`)
	h.Set("Content-Type", ctype)
	part, _ := mw.CreatePart(h)
	part.Write(data)
	mw.Close()
	req := httptest.NewRequest("POST", "/dokumente", &body)
	req.Host = "haus.box.test"
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Remote-User", "anna")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	Dokumente(cfg).ServeHTTP(rec, req)
	return rec
}

func TestDokumenteFilesIntoPaperlessInbox(t *testing.T) {
	dir, cfg := dokumente(t)
	pdf := []byte("%PDF-1.7\n1 0 obj<<>>endobj\n%%EOF\n")
	rec := upload(t, cfg, "application/pdf", pdf, "Stromrechnung Oktober", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/dokumente?abgelegt=1" {
		t.Fatalf("%d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	files, _ := filepath.Glob(filepath.Join(dir, "Stromrechnung Oktober-*.pdf"))
	if len(files) != 1 {
		es, _ := os.ReadDir(dir)
		t.Fatalf("in the inbox: %v", es)
	}
	if b, _ := os.ReadFile(files[0]); !bytes.Equal(b, pdf) {
		t.Fatal("not the bytes sent")
	}
	if page := getDokumente(cfg, "/dokumente?abgelegt=1", nil).Body.String(); !strings.Contains(page, "Liegt in Paperless") {
		t.Errorf("the page says so:\n%s", page)
	}
	// what Paperless does not take: back with the reason in the family's words, nothing stored
	rec = upload(t, cfg, "image/heic", []byte("ftypheic...."), "", nil)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/dokumente?fehler=") {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
	if page := getDokumente(cfg, rec.Header().Get("Location"), nil).Body.String(); !strings.Contains(page, "nur Fotos (JPG, PNG) und PDFs") || !strings.Contains(page, "class=achtung") {
		t.Errorf("the reason:\n%s", page)
	}
	if page := getDokumente(cfg, "/dokumente?fehler=whatever", nil).Body.String(); !strings.Contains(page, saysUploadFailed) {
		t.Errorf("an unknown reason: %s", page)
	}
	// from another site, or without the upload folder: nothing stored
	if rec := upload(t, cfg, "application/pdf", pdf, "x", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d", rec.Code)
	}
	if rec := upload(t, StartConfig{DocumentsMail: "d@box.test"}, "application/pdf", pdf, "x", nil); rec.Code != http.StatusNotFound {
		t.Errorf("without HAUS_UPLOAD_DIR: %d", rec.Code)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "*")); len(files) != 1 {
		t.Errorf("only the first is there: %v", files)
	}
}

func TestDokumenteIsInMehrAndTheBareDocumentPathLeadsThere(t *testing.T) {
	_, cfg := dokumente(t)
	req := httptest.NewRequest("GET", "/mehr", nil)
	req.Header.Set("Remote-User", "anna")
	rec := httptest.NewRecorder()
	Mehr(cfg).ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `href="/dokumente"`) && !strings.Contains(rec.Body.String(), "href=/dokumente") {
		t.Errorf("Mehr lacks Dokumente:\n%s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	Mehr(StartConfig{}).ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "/dokumente") {
		t.Error("Dokumente without any way in")
	}
	rec = httptest.NewRecorder()
	Dokument(cfg).ServeHTTP(rec, httptest.NewRequest("GET", "/dokumente/", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/dokumente" {
		t.Errorf("/dokumente/: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestMailAddress(t *testing.T) {
	for raw, want := range map[string]string{"dokumente@dopp.cloud": "dokumente@dopp.cloud", " dokumente@dopp.cloud ": "dokumente@dopp.cloud",
		"Archiv <a@b.c>": "", "a@b.c?subject=x": "", "": "", "kein": "", "a@b@c": ""} {
		if got := MailAddress(raw); got != want {
			t.Errorf("MailAddress(%q) = %q, want %q", raw, got, want)
		}
	}
}
