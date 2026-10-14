package napi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/laib"
)

// haus#10, slice 2: the camera capture (CameraCaptureActivity.kt, ApiClient.uploadFile)
// lands in the share's consume folder: multipart, a "filename" part and a "file"
// part, JPEG or PDF. Haus writes a request whole or not at all, and never over
// another file.

var pdf = []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n")

type part struct {
	name, filename, ctype string
	body                  []byte
}

func multipartBody(t *testing.T, parts ...part) (string, *bytes.Buffer) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for _, p := range parts {
		if p.filename == "" && p.ctype == "" {
			mw.WriteField(p.name, string(p.body))
			continue
		}
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition", `form-data; name="`+p.name+`"; filename="`+p.filename+`"`)
		hdr.Set("Content-Type", p.ctype)
		w, err := mw.CreatePart(hdr)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(p.body)
	}
	mw.Close()
	return mw.FormDataContentType(), &b
}

func uploader(t *testing.T) (string, http.Handler) {
	t.Helper()
	var calls []string
	lb := fakeLaib(t, &calls)
	t.Cleanup(lb.Close)
	dir := t.TempDir()
	return dir, Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"}), UploadDir: dir})
}

func upload(t *testing.T, h http.Handler, bearer, ctype string, body io.Reader) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/napi/upload", body)
	req.Header.Set("Content-Type", ctype)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func files(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(dir, p)
			out[rel] = b
		}
		return nil
	})
	return out
}

func TestUploadStoresTheCapture(t *testing.T) {
	dir, h := uploader(t)
	ct, body := multipartBody(t, part{name: "filename", body: []byte("2026-10-14 Rechnung.pdf")},
		part{name: "file", filename: "2026-10-14 Rechnung.pdf", ctype: "application/pdf", body: pdf})
	code, out := upload(t, h, "dev1", ct, body)
	if code != 200 || out["ok"] != true {
		t.Fatalf("upload: %d %v", code, out)
	}
	fs := files(t, dir)
	if len(fs) != 1 {
		t.Fatalf("one file, no leftovers: %v", keys(fs))
	}
	name := keys(fs)[0]
	if !regexp.MustCompile(`^2026-10-14 Rechnung-[0-9a-f]{8}\.pdf$`).MatchString(name) || !bytes.Equal(fs[name], pdf) {
		t.Fatalf("stored as %q", name)
	}
	if out["id"] != name {
		t.Fatalf("id names the file: %v", out)
	}
	noNull(t, "upload", out)

	// the same name again is a second file, never the first one replaced
	ct, body = multipartBody(t, part{name: "filename", body: []byte("2026-10-14 Rechnung.pdf")},
		part{name: "file", filename: "x.pdf", ctype: "application/pdf", body: pdf})
	if code, _ := upload(t, h, "dev1", ct, body); code != 200 {
		t.Fatalf("second: %d", code)
	}
	if fs := files(t, dir); len(fs) != 2 {
		t.Fatalf("two uploads, two files: %v", keys(fs))
	}
}

func TestUploadSeveralPagesAndNames(t *testing.T) {
	dir, h := uploader(t)
	ct, body := multipartBody(t,
		part{name: "file", filename: "../../etc/passwd", ctype: "image/jpeg", body: jpeg},
		part{name: "file", filename: ".versteckt.jpg", ctype: "image/jpeg", body: jpeg},
		part{name: "kind", body: []byte("document")})
	code, out := upload(t, h, "dev1", ct, body)
	fl, _ := out["files"].([]any)
	if code != 200 || out["ok"] != true || len(fl) != 2 {
		t.Fatalf("two files: %d %v", code, out)
	}
	for name := range files(t, dir) {
		if strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".jpg") {
			t.Fatalf("a name from the phone is a name, not a path, and never hidden: %q", name)
		}
	}
	// a name with nothing usable left is still a name
	ct, body = multipartBody(t, part{name: "file", filename: "///", ctype: "image/jpeg", body: jpeg})
	if code, _ := upload(t, h, "dev1", ct, body); code != 200 {
		t.Fatalf("an empty name: %d", code)
	}
}

func TestUploadRefuses(t *testing.T) {
	dir, h := uploader(t)
	defer func(n int64) { uploadMaxFile = n }(uploadMaxFile)
	uploadMaxFile = 1 << 10
	for _, c := range []struct {
		parts []part
		code  int
		why   string
	}{
		{[]part{{name: "file", filename: "a.txt", ctype: "text/plain", body: []byte("hallo")}}, 415, "text is not a document"},
		{[]part{{name: "file", filename: "a.svg", ctype: "image/svg+xml", body: []byte("<svg onload=alert(1)/>")}}, 415, "an SVG carries script"},
		{[]part{{name: "file", filename: "a.jpg", ctype: "image/jpeg", body: []byte("<html><script>x</script></html>")}}, 415, "what it says it is, and is"},
		{[]part{{name: "file", filename: "a.pdf", ctype: "application/pdf", body: jpeg}}, 415, "a JPEG is no PDF"},
		{[]part{{name: "file", filename: "a.jpg", ctype: "image/jpeg", body: append(append([]byte{}, jpeg...), bytes.Repeat([]byte{1}, 2<<10)...)}}, 413, "larger than the cap"},
		{[]part{{name: "file", filename: "a.jpg", ctype: "image/jpeg"}}, 400, "an empty file"},
		{[]part{{name: "filename", body: []byte("nur ein Name")}}, 400, "no file at all"},
	} {
		ct, body := multipartBody(t, c.parts...)
		if code, out := upload(t, h, "dev1", ct, body); code != c.code || out["ok"] != false {
			t.Errorf("%s: %d %v, want %d", c.why, code, out, c.code)
		}
	}
	var many []part
	for i := 0; i < 21; i++ {
		many = append(many, part{name: "file", filename: "p.jpg", ctype: "image/jpeg", body: jpeg})
	}
	ct, body := multipartBody(t, many...)
	if code, _ := upload(t, h, "dev1", ct, body); code != 400 {
		t.Errorf("21 files: %d, want 400", code)
	}
	if code, _ := upload(t, h, "dev1", "application/json", strings.NewReader(`{}`)); code != 400 {
		t.Errorf("not multipart: %d", code)
	}
	if fs := files(t, dir); len(fs) != 0 {
		t.Errorf("a refused upload is all refused, nothing left: %v", keys(fs))
	}
}

func TestUploadWantsAPairedDevice(t *testing.T) {
	dir, h := uploader(t)
	for _, bearer := range []string{"", "wrong"} {
		ct, body := multipartBody(t, part{name: "file", filename: "a.jpg", ctype: "image/jpeg", body: jpeg})
		if code, _ := upload(t, h, bearer, ct, body); code != 401 {
			t.Errorf("upload with %q → %d", bearer, code)
		}
	}
	if fs := files(t, dir); len(fs) != 0 {
		t.Fatalf("stored without a token: %v", keys(fs))
	}
}

func TestWithoutAnUploadDirTheUploadGoesThroughTheBridge(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	var got int
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = len(b)
		w.Write([]byte(`{"ok":true,"via":"bridge"}`))
	}))
	defer bridge.Close()
	u, _ := url.Parse(bridge.URL)
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"}), Bridge: u})
	ct, body := multipartBody(t, part{name: "file", filename: "a.jpg", ctype: "image/jpeg", body: jpeg})
	if code, out := upload(t, h, "dev1", ct, body); code != 200 || out["via"] != "bridge" || got == 0 {
		t.Fatalf("no upload dir: %d %v", code, out)
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
