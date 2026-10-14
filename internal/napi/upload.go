package napi

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strings"
	"unicode"
)

// The camera capture (haus#10): the app posts a photo or a PDF it made
// (CameraCaptureActivity.kt, ApiClient.uploadFile: multipart, a "filename"
// part and a "file" part) and Haus puts it into HAUS_UPLOAD_DIR, the
// share's folder Paperless consumes. A file appears there whole or not at
// all: written aside under a name Paperless ignores (._*), then hard-linked
// to its name, which never replaces an existing file. A request is stored
// all or nothing.

var (
	uploadMaxFile  int64 = 25 << 20 // per file, the contract's 25 MB
	uploadMaxBody  int64 = 64 << 20 // per request; the app sends one file per request
	uploadMaxFiles       = 20
)

// uploadTypes are what Paperless consumes and the app may send, with the
// extension Haus gives them. The bytes must say the same (sniffed); an
// image/* outside this list (an SVG carries script) is 415.
var uploadTypes = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp", "application/pdf": ".pdf",
}

type uploads struct{ dir string }

func (u *uploads) routes(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /napi/upload", auth(u.upload))
}

// the reasons a part is refused, each with its status
type uploadErr struct {
	code int
	msg  string
}

func (e *uploadErr) Error() string { return e.msg }

var (
	errTooLarge = &uploadErr{http.StatusRequestEntityTooLarge, "file too large"}
	errEmpty    = &uploadErr{http.StatusBadRequest, "empty file"}
	errType     = &uploadErr{http.StatusUnsupportedMediaType, "unsupported type"}
)

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// staged is one part written aside, waiting for the rest of the request.
type staged struct{ tmp, name, ext string }

// POST /napi/upload → {ok, id} for one file, {ok, files:[{ok, id}]} for several.
func (u *uploads) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, uploadMaxBody)
	mr, err := r.MultipartReader()
	if err != nil {
		refuse(w, http.StatusBadRequest, "expected multipart/form-data")
		return
	}
	root, err := os.OpenRoot(u.dir)
	if err != nil {
		log.Printf("haus: upload: %v", err)
		refuse(w, http.StatusServiceUnavailable, "upload_unavailable")
		return
	}
	defer root.Close()
	var parts []staged
	defer func() {
		for _, p := range parts {
			root.Remove(p.tmp)
		}
	}()
	fail := func(err error) {
		var ue *uploadErr
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &ue):
			refuse(w, ue.code, ue.msg)
		case errors.As(err, &mbe):
			refuse(w, http.StatusRequestEntityTooLarge, "request too large")
		default:
			log.Printf("haus: upload: %v", err)
			refuse(w, http.StatusBadRequest, "malformed multipart")
		}
	}
	chosen := ""
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			fail(err)
			return
		}
		switch p.FormName() {
		case "filename":
			b, err := io.ReadAll(io.LimitReader(p, 1<<10))
			if err != nil {
				fail(err)
				return
			}
			chosen = string(b)
		case "file":
			if len(parts) == uploadMaxFiles {
				refuse(w, http.StatusBadRequest, "too many files")
				return
			}
			s, err := stage(root, p)
			if s.tmp != "" {
				parts = append(parts, s)
			}
			if err != nil {
				fail(err)
				return
			}
		default:
			if _, err := io.Copy(io.Discard, p); err != nil {
				fail(err)
				return
			}
		}
	}
	if len(parts) == 0 {
		refuse(w, http.StatusBadRequest, "no file part")
		return
	}
	var placed []string
	for _, p := range parts {
		name := p.name
		if chosen != "" {
			name = chosen
		}
		final, err := place(root, p.tmp, stem(name), p.ext)
		if err != nil {
			log.Printf("haus: upload into %s: %v", u.dir, err)
			for _, f := range placed { // all or nothing: these are ours, just made
				root.Remove(f)
			}
			refuse(w, http.StatusInternalServerError, "store_failed")
			return
		}
		placed = append(placed, final)
	}
	log.Printf("haus: upload stored %s", strings.Join(placed, ", "))
	if len(placed) == 1 {
		answer(w, http.StatusOK, map[string]any{"ok": true, "id": placed[0]})
		return
	}
	files := []map[string]any{}
	for _, f := range placed {
		files = append(files, map[string]any{"ok": true, "id": f})
	}
	answer(w, http.StatusOK, map[string]any{"ok": true, "files": files})
}

// stage writes one file part aside, checked: an allowed type whose first
// bytes are that type, not empty, not over the cap. tmp is set as soon as
// a file exists, so the caller removes it whatever happens.
func stage(root *os.Root, p *multipart.Part) (staged, error) {
	ctype, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
	ext, ok := uploadTypes[ctype]
	if !ok {
		return staged{}, errType
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(p, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return staged{}, err
	}
	head = head[:n]
	if n == 0 {
		return staged{}, errEmpty
	}
	if sniffed, _, _ := mime.ParseMediaType(http.DetectContentType(head)); sniffed != ctype {
		return staged{}, errType
	}
	s := staged{tmp: "._haus-" + randHex(8) + ".part", name: p.FileName(), ext: ext}
	f, err := root.OpenFile(s.tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return staged{}, err
	}
	written, err := io.Copy(f, io.LimitReader(io.MultiReader(bytes.NewReader(head), p), uploadMaxFile+1))
	if err == nil && written > uploadMaxFile {
		err = errTooLarge
	}
	if serr := f.Sync(); err == nil {
		err = serr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return s, err
}

// place links tmp to <stem>-<random>.<ext>; a name that exists is never
// replaced, another random part is tried. A share without hard links is
// an error, not a weaker write.
func place(root *os.Root, tmp, stem, ext string) (string, error) {
	for range 8 {
		name := stem + "-" + randHex(4) + ext
		err := root.Link(tmp, name)
		if err == nil {
			return name, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("no free name for %s", stem)
}

// stem is the phone's name for a file as a plain name: no folder, no known
// extension, letters, digits, umlauts, space and ._- only, never hidden,
// at most 80 characters; "upload" when nothing is left.
func stem(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	if name == "/" || name == "." {
		name = ""
	}
	low := strings.ToLower(name)
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".pdf"} {
		if strings.HasSuffix(low, ext) {
			name = name[:len(name)-len(ext)]
			break
		}
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" ._-", r)):
			return r
		case strings.ContainsRune("äöüÄÖÜß", r):
			return r
		}
		return '_'
	}, name)
	name = strings.Trim(name, " ._-")
	if r := []rune(name); len(r) > 80 {
		name = strings.TrimRight(string(r[:80]), " ._-")
	}
	if name == "" {
		return "upload"
	}
	return name
}
