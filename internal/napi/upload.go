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
	"syscall"
	"time"
	"unicode"
)

// The camera capture (haus#10): the app posts a photo or a PDF it made
// (CameraCaptureActivity.kt, ApiClient.uploadFile: multipart, a "filename"
// part and a "file" part) and Haus puts it into HAUS_UPLOAD_DIR, the
// share's folder Paperless consumes. A file appears there whole or not at
// all: written aside under a name Paperless ignores (._*), then renamed to
// its name with renameat2(RENAME_NOREPLACE), which never replaces an
// existing file and which Paperless's inotify consumer sees (MOVED_TO; a
// hard link fires only IN_CREATE). Where the kernel or the file system has
// no RENAME_NOREPLACE, a hard link does the same, minus that event. A
// request is stored all or nothing.

var (
	uploadMaxFile  int64 = 25 << 20 // per file, the contract's 25 MB
	uploadMaxBody  int64 = 64 << 20 // per request; the app sends one file per request
	uploadMaxFiles       = 20
	// uploads running at once; one more is 429
	uploadConcurrent = 2
	// how long one request may take to arrive: a phone on a slow uplink
	// sends 25 MB in well under this, a stalled sender lets go of its slot
	uploadReadTimeout = 2 * time.Minute
)

// uploadMinFree is the room the share must have before an upload is read.
func uploadMinFree() uint64 { return 2 * uint64(uploadMaxBody) }

// uploadTypes are what Paperless consumes and the app may send, with the
// extension Haus gives them. The bytes must say the same (sniffed); an
// image/* outside this list (an SVG carries script) is 415.
var uploadTypes = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp", "application/pdf": ".pdf",
}

// what a file goes through on its way to the share; variables, so the tests
// can make a disk fail or a name collide
var (
	randHex = func(n int) string {
		b := make([]byte, n)
		rand.Read(b)
		return hex.EncodeToString(b)
	}
	createStage = func(root *os.Root, name string) (stageFile, error) {
		return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	}
	noReplace = renameNoReplace // rename_linux.go; errNoRenameat2 where there is none
	linkFile  = func(root *os.Root, oldname, newname string) error { return root.Link(oldname, newname) }
	freeSpace = statfsFree
)

// stageFile is what a staged part is written to.
type stageFile interface {
	io.Writer
	Sync() error
	Close() error
}

// stagePrefix names a part written aside; Paperless ignores ._* files.
const stagePrefix = "._haus-"

// SweepUploads removes parts a stopped Haus left staged in dir.
func SweepUploads(dir string) error {
	es, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range es {
		if n := e.Name(); e.Type().IsRegular() && strings.HasPrefix(n, stagePrefix) && strings.HasSuffix(n, ".part") {
			if err := os.Remove(dir + "/" + n); err != nil {
				return err
			}
		}
	}
	return nil
}

type uploads struct {
	dir   string
	slots chan struct{}
}

func newUploads(dir string) *uploads {
	return &uploads{dir: dir, slots: make(chan struct{}, uploadConcurrent)}
}

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

// diskErr is the share's own trouble while staging: the server's, not the phone's.
type diskErr struct{ err error }

func (e *diskErr) Error() string { return e.err.Error() }
func (e *diskErr) Unwrap() error { return e.err }

// diskWriter marks its writer's errors as the disk's.
type diskWriter struct{ f stageFile }

func (d diskWriter) Write(p []byte) (int, error) {
	n, err := d.f.Write(p)
	if err != nil {
		err = &diskErr{err}
	}
	return n, err
}

// staged is one part written aside, waiting for the rest of the request.
type staged struct{ tmp, name, ext string }

// POST /napi/upload → {ok, id} for one file, {ok, files:[{ok, id}]} for several.
func (u *uploads) upload(w http.ResponseWriter, r *http.Request) {
	placed, code, reason := u.store(w, r)
	if code != 0 {
		refuse(w, code, reason)
		return
	}
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

// Uploads is the same way into Paperless's inbox for Haus's own Dokumente
// page (haus#68): one request's files, all or nothing, as the app's.
type Uploads struct{ u *uploads }

// NewUploads stores into dir, the share's consume folder.
func NewUploads(dir string) *Uploads { return &Uploads{newUploads(dir)} }

// Store reads r's multipart form ("file" parts, an optional "filename")
// into the folder: the names it placed, or the status and the reason
// (napi's words, "unsupported type", "busy" …) when nothing was stored.
func (u *Uploads) Store(w http.ResponseWriter, r *http.Request) ([]string, int, string) {
	return u.u.store(w, r)
}

// store is one upload request: the names placed, or a status and its
// reason (code 0 when stored).
func (u *uploads) store(w http.ResponseWriter, r *http.Request) ([]string, int, string) {
	select {
	case u.slots <- struct{}{}:
		defer func() { <-u.slots }()
	default:
		return nil, http.StatusTooManyRequests, "busy"
	}
	if free, err := freeSpace(u.dir); err != nil {
		log.Printf("haus: upload: free space of %s: %v", u.dir, err)
	} else if free < uploadMinFree() {
		log.Printf("haus: upload: %s has %d bytes free, wants %d", u.dir, free, uploadMinFree())
		return nil, http.StatusInsufficientStorage, "insufficient_storage"
	}
	// not supported by every ResponseWriter (a test recorder); the server's is
	http.NewResponseController(w).SetReadDeadline(time.Now().Add(uploadReadTimeout))
	r.Body = http.MaxBytesReader(w, r.Body, uploadMaxBody)
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, http.StatusBadRequest, "expected multipart/form-data"
	}
	root, err := os.OpenRoot(u.dir)
	if err != nil {
		log.Printf("haus: upload: %v", err)
		return nil, http.StatusServiceUnavailable, "upload_unavailable"
	}
	defer root.Close()
	var parts []staged
	defer func() {
		for _, p := range parts {
			root.Remove(p.tmp) // gone already once placed by rename
		}
	}()
	fail := func(err error) (int, string) {
		var ue *uploadErr
		var mbe *http.MaxBytesError
		var de *diskErr
		switch {
		case errors.As(err, &ue):
			return ue.code, ue.msg
		case errors.As(err, &mbe):
			return http.StatusRequestEntityTooLarge, "request too large"
		case errors.As(err, &de):
			log.Printf("haus: upload into %s: %v", u.dir, err)
			if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
				return http.StatusInsufficientStorage, "insufficient_storage"
			}
			return http.StatusInternalServerError, "store_failed"
		case errors.Is(err, os.ErrDeadlineExceeded):
			return http.StatusRequestTimeout, "too_slow"
		default:
			log.Printf("haus: upload: %v", err)
			return http.StatusBadRequest, "malformed multipart"
		}
	}
	chosen := ""
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			code, why := fail(err)
			return nil, code, why
		}
		switch p.FormName() {
		case "filename":
			b, err := io.ReadAll(io.LimitReader(p, 1<<10))
			if err != nil {
				code, why := fail(err)
				return nil, code, why
			}
			chosen = string(b)
		case "file":
			if len(parts) == uploadMaxFiles {
				return nil, http.StatusBadRequest, "too many files"
			}
			s, err := stage(root, p)
			if s.tmp != "" {
				parts = append(parts, s)
			}
			if err != nil {
				code, why := fail(err)
				return nil, code, why
			}
		default:
			if _, err := io.Copy(io.Discard, p); err != nil {
				code, why := fail(err)
				return nil, code, why
			}
		}
	}
	if len(parts) == 0 {
		return nil, http.StatusBadRequest, "no file part"
	}
	dir, err := root.Open(".")
	if err != nil {
		code, why := fail(&diskErr{err})
		return nil, code, why
	}
	defer dir.Close()
	var placed []string
	for _, p := range parts {
		name := p.name
		if strings.TrimSpace(chosen) != "" {
			name = chosen
		}
		final, err := place(root, dir, p.tmp, stem(name), p.ext)
		if err != nil {
			log.Printf("haus: upload into %s: %v", u.dir, err)
			for _, f := range placed { // all or nothing: these are ours, just made
				root.Remove(f)
			}
			return nil, http.StatusInternalServerError, "store_failed"
		}
		placed = append(placed, final)
	}
	log.Printf("haus: upload stored %s", strings.Join(placed, ", "))
	return placed, 0, ""
}

// stage writes one file part aside, checked: an allowed type whose first
// bytes are that type, not empty, not over the cap. tmp is set as soon as
// a file exists, so the caller removes it whatever happens. The disk's
// own errors come back as *diskErr.
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
	s := staged{tmp: stagePrefix + randHex(8) + ".part", name: p.FileName(), ext: ext}
	f, err := createStage(root, s.tmp)
	if err != nil {
		return staged{}, &diskErr{err}
	}
	written, err := io.Copy(diskWriter{f}, io.LimitReader(io.MultiReader(bytes.NewReader(head), p), uploadMaxFile+1))
	if err == nil && written > uploadMaxFile {
		err = errTooLarge
	}
	if serr := f.Sync(); err == nil && serr != nil {
		err = &diskErr{serr}
	}
	if cerr := f.Close(); err == nil && cerr != nil {
		err = &diskErr{cerr}
	}
	return s, err
}

// place moves tmp to <stem>-<random>.<ext>; a name that exists is never
// replaced, another random part is tried. Without RENAME_NOREPLACE a hard
// link does it; a share with neither is an error, not a weaker write.
func place(root *os.Root, dir *os.File, tmp, stem, ext string) (string, error) {
	for range 8 {
		name := stem + "-" + randHex(4) + ext
		err := noReplace(dir, tmp, name)
		if errors.Is(err, errNoRenameat2) {
			if err = linkFile(root, tmp, name); err == nil {
				root.Remove(tmp)
			}
		}
		if err == nil {
			return name, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("no free name for %s", stem)
}

// decomposed umlauts (a phone's NFD) as the precomposed ones; the standard
// library has no normaliser, and these are the ones a German name carries
var nfd = strings.NewReplacer("ä", "ä", "ö", "ö", "ü", "ü", "Ä", "Ä", "Ö", "Ö", "Ü", "Ü")

// stem is the phone's name for a file as a plain name: no folder, no known
// extension, letters, digits, umlauts, space and ._- only (anything else,
// a bidi override included, is '_'), never hidden, at most 80 characters;
// "upload" when nothing is left.
func stem(name string) string {
	name = path.Base(strings.ReplaceAll(nfd.Replace(name), `\`, "/"))
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
