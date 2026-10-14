package napi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

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

func TestWithoutAnUploadDirTheUploadIs404(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"})})
	ct, body := multipartBody(t, part{name: "file", filename: "a.jpg", ctype: "image/jpeg", body: jpeg})
	if code, out := upload(t, h, "dev1", ct, body); code != 404 || out["ok"] != false || out["reason"] != "not_found" {
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

// Review of slice 2: how a file lands, what a broken disk answers, and how
// far one phone (or anyone with a token) can push the share.

func stubRand(t *testing.T, fours ...string) {
	t.Helper()
	real := randHex
	t.Cleanup(func() { randHex = real })
	randHex = func(n int) string {
		if n == 4 && len(fours) > 0 {
			v := fours[0]
			fours = fours[1:]
			return v
		}
		return real(n)
	}
}

func oneJPEG(t *testing.T, name string) (string, *bytes.Buffer) {
	return multipartBody(t, part{name: "filename", body: []byte(name)}, part{name: "file", filename: name, ctype: "image/jpeg", body: jpeg})
}

// A name that is taken is never replaced, on the rename path (renameat2
// RENAME_NOREPLACE, which Paperless's inotify sees as MOVED_TO) and on the
// hard-link fallback for a kernel or file system without it.
func TestUploadNeverReplaces(t *testing.T) {
	for _, viaLink := range []bool{false, true} {
		dir, h := uploader(t)
		if err := os.WriteFile(filepath.Join(dir, "x-aaaaaaaa.jpg"), []byte("first"), 0o644); err != nil {
			t.Fatal(err)
		}
		stubRand(t, "aaaaaaaa", "bbbbbbbb")
		realRename, realLink := noReplace, linkFile
		usedLink := false
		linkFile = func(r *os.Root, o, n string) error { usedLink = true; return realLink(r, o, n) }
		if viaLink {
			noReplace = func(*os.File, string, string) error { return errNoRenameat2 }
		}
		ct, body := oneJPEG(t, "x.jpg")
		code, out := upload(t, h, "dev1", ct, body)
		noReplace, linkFile = realRename, realLink
		if code != 200 || out["id"] != "x-bbbbbbbb.jpg" {
			t.Fatalf("link %v: %d %v", viaLink, code, out)
		}
		if usedLink != viaLink {
			t.Fatalf("link %v: the link path was used: %v", viaLink, usedLink)
		}
		fs := files(t, dir)
		if string(fs["x-aaaaaaaa.jpg"]) != "first" || !bytes.Equal(fs["x-bbbbbbbb.jpg"], jpeg) || len(fs) != 2 {
			t.Fatalf("link %v: %v", viaLink, keys(fs))
		}
	}
}

func TestUploadStagesUnderAName_PaperlessIgnores(t *testing.T) {
	_, h := uploader(t)
	real := createStage
	defer func() { createStage = real }()
	var names []string
	createStage = func(r *os.Root, name string) (stageFile, error) {
		names = append(names, name)
		return real(r, name)
	}
	ct, body := oneJPEG(t, "a.jpg")
	if code, _ := upload(t, h, "dev1", ct, body); code != 200 {
		t.Fatalf("upload: %d", code)
	}
	if len(names) != 1 || !regexp.MustCompile(`^\._haus-[0-9a-f]{16}\.part$`).MatchString(names[0]) {
		t.Fatalf("staged as %v", names)
	}
}

func TestUploadRequestCapAndNames(t *testing.T) {
	dir, h := uploader(t)
	defer func(n int64) { uploadMaxBody = n }(uploadMaxBody)
	uploadMaxBody = 4 << 10
	ct, body := multipartBody(t, part{name: "file", filename: "a.jpg", ctype: "image/jpeg", body: append(append([]byte{}, jpeg...), bytes.Repeat([]byte{1}, 3<<10)...)},
		part{name: "file", filename: "b.jpg", ctype: "image/jpeg", body: append(append([]byte{}, jpeg...), bytes.Repeat([]byte{1}, 3<<10)...)})
	if code, out := upload(t, h, "dev1", ct, body); code != 413 || out["ok"] != false {
		t.Fatalf("request over the cap: %d %v", code, out)
	}
	if fs := files(t, dir); len(fs) != 0 {
		t.Fatalf("left: %v", keys(fs))
	}
	uploadMaxBody = 64 << 20
	for name, want := range map[string]string{
		"a:b*?\u202ec.jpg":     `^a_b___c-[0-9a-f]{8}\.jpg$`,
		"Rechnung Müller.pdf": `^Rechnung Müller-[0-9a-f]{8}\.jpg$`,
	} {
		ct, body := oneJPEG(t, name)
		code, out := upload(t, h, "dev1", ct, body)
		id, _ := out["id"].(string)
		if code != 200 || !regexp.MustCompile(want).MatchString(id) {
			t.Errorf("%q stored as %q (%d)", name, id, code)
		}
	}
}

func TestUploadIsAllOrNothing(t *testing.T) {
	dir, h := uploader(t)
	real := noReplace
	defer func() { noReplace = real }()
	n := 0
	noReplace = func(d *os.File, o, nw string) error {
		if n++; n == 2 {
			return syscall.EPERM
		}
		return real(d, o, nw)
	}
	ct, body := multipartBody(t, part{name: "file", filename: "a.jpg", ctype: "image/jpeg", body: jpeg}, part{name: "file", filename: "b.jpg", ctype: "image/jpeg", body: jpeg})
	if code, out := upload(t, h, "dev1", ct, body); code != 500 || out["error"] != "store_failed" {
		t.Fatalf("second place fails: %d %v", code, out)
	}
	if fs := files(t, dir); len(fs) != 0 {
		t.Fatalf("the first stayed: %v", keys(fs))
	}
}

type brokenFile struct{ write, sync error }

func (b brokenFile) Write(p []byte) (int, error) {
	if b.write != nil {
		return 0, b.write
	}
	return len(p), nil
}
func (b brokenFile) Sync() error  { return b.sync }
func (b brokenFile) Close() error { return nil }

// The disk's own trouble is the server's, not the phone's bad request.
func TestUploadDiskErrors(t *testing.T) {
	_, h := uploader(t)
	real := createStage
	defer func() { createStage = real }()
	for _, c := range []struct {
		open        error
		write, sync error
		code        int
	}{
		{write: syscall.ENOSPC, code: 507},
		{write: syscall.EDQUOT, code: 507},
		{sync: syscall.EIO, code: 500},
		{open: syscall.EACCES, code: 500},
	} {
		createStage = func(*os.Root, string) (stageFile, error) {
			if c.open != nil {
				return nil, c.open
			}
			return brokenFile{c.write, c.sync}, nil
		}
		ct, body := oneJPEG(t, "a.jpg")
		if code, out := upload(t, h, "dev1", ct, body); code != c.code || out["ok"] != false {
			t.Errorf("%+v: %d %v", c, code, out)
		}
	}
}

func TestUploadWantsRoomOnTheShare(t *testing.T) {
	dir, h := uploader(t)
	real := freeSpace
	defer func() { freeSpace = real }()
	freeSpace = func(string) (uint64, error) { return uint64(uploadMaxBody), nil }
	ct, body := oneJPEG(t, "a.jpg")
	if code, out := upload(t, h, "dev1", ct, body); code != 507 || out["ok"] != false {
		t.Fatalf("a nearly full share: %d %v", code, out)
	}
	if fs := files(t, dir); len(fs) != 0 {
		t.Fatalf("written: %v", keys(fs))
	}
}

func TestUploadsAtOnceAreFew(t *testing.T) {
	_, h := uploader(t)
	var writers []*io.PipeWriter
	done := make(chan struct{}, uploadConcurrent)
	for i := 0; i < uploadConcurrent; i++ {
		pr, pw := io.Pipe()
		writers = append(writers, pw)
		go func() {
			req := httptest.NewRequest("POST", "/napi/upload", pr)
			req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
			req.Header.Set("Authorization", "Bearer dev1")
			h.ServeHTTP(httptest.NewRecorder(), req)
			done <- struct{}{}
		}()
	}
	time.Sleep(200 * time.Millisecond) // both hold a slot, blocked on their bodies
	ct, body := oneJPEG(t, "a.jpg")
	got, _ := upload(t, h, "dev1", ct, body)
	for _, w := range writers {
		w.Close()
	}
	for i := 0; i < uploadConcurrent; i++ {
		<-done
	}
	if got != 429 {
		t.Fatalf("with %d uploads running, another got %d, want 429", uploadConcurrent, got)
	}
	ct, body = oneJPEG(t, "a.jpg")
	if code, _ := upload(t, h, "dev1", ct, body); code != 200 {
		t.Fatalf("after them: %d", code)
	}
}

// A phone (or anyone with a token) that stops sending holds a slot only
// until the read deadline.
func TestUploadReadDeadline(t *testing.T) {
	dir, h := uploader(t)
	defer func(d time.Duration) { uploadReadTimeout = d }(uploadReadTimeout)
	uploadReadTimeout = 300 * time.Millisecond
	srv := httptest.NewServer(h)
	defer srv.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ct, body := oneJPEG(t, "a.jpg")
	head := "POST /napi/upload HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer dev1\r\nContent-Type: " + ct + "\r\nContent-Length: " + strconv.Itoa(body.Len()) + "\r\n\r\n"
	conn.Write([]byte(head))
	conn.Write(body.Bytes()[:body.Len()/2]) // and then nothing
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("no answer: %v", err)
	}
	if resp.StatusCode != 408 {
		t.Fatalf("a stalled upload: %d", resp.StatusCode)
	}
	if fs := files(t, dir); len(fs) != 0 {
		t.Fatalf("left: %v", keys(fs))
	}
}

func TestSweepUploads(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"._haus-0123456789abcdef.part", "._andere", "rechnung.pdf"} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	if err := SweepUploads(dir); err != nil {
		t.Fatal(err)
	}
	fs := files(t, dir)
	if _, ok := fs["._haus-0123456789abcdef.part"]; ok || len(fs) != 2 {
		t.Fatalf("after the sweep: %v", keys(fs))
	}
}
