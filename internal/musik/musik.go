// Package musik is beets' inbox as the app fills it (haus#107, laib templates/beets
// "Uploading from Homes"; owner 2026-11-04: by Teilen → Homes): each file is one upload at
// the top, written as ._<name>.….part (the importer skips dot files), the uploader's sidecar
// ._<name>.von beside it, then renamed to its name: whole or not at all, never over another.
package musik

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// MaxDatei is the most one file may be: an album's ZIP, or several.
const MaxDatei = int64(4) << 30

// Name is a phone's name for a file as an upload's: no folder, not
// hidden, a music file or a ZIP; "" when it is none.
func Name(n string) string {
	n = path.Base(strings.ReplaceAll(n, `\`, "/"))
	if strings.HasPrefix(n, ".") || n == "/" || strings.ContainsRune(n, 0) || len(n) > 255 || !strings.Contains(" .mp3 .flac .m4a .ogg .opus .wav .zip ", " "+strings.ToLower(path.Ext(n))+" ") {
		return ""
	}
	return n
}

// Uploader is the signed-in user as the importer takes it: lower case,
// [a-z0-9._@-], at most 64 characters; else "" (no sidecar).
func Uploader(user string) string {
	u := strings.ToLower(strings.TrimSpace(user))
	if u == "" || len(u) > 64 || strings.Trim(u, "abcdefghijklmnopqrstuvwxyz0123456789._@-") != "" {
		return ""
	}
	return u
}

var (
	errGross = errors.New("Die Datei ist zu groß: höchstens 4 GB.")
	errLeer  = errors.New("Die Datei ist leer.")
	errOft   = errors.New("Diese Datei liegt schon zu oft im Musik-Eingang.")
)

// Ablegen puts body into the inbox dir as name (Name's) for person: the
// name it got, name or "name (2)" beside one there already.
func Ablegen(dir, name, person string, body io.Reader) (string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	part := "._" + name + "." + strconv.FormatInt(time.Now().UnixNano(), 36) + ".part" // two phones, one name: two parts
	f, err := root.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o664)
	if err != nil {
		return "", err
	}
	defer root.Remove(part) // gone already once renamed
	n, err := io.Copy(f, io.LimitReader(body, MaxDatei+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return "", err
	case n > MaxDatei:
		return "", errGross
	case n == 0:
		return "", errLeer
	}
	final, ext := name, path.Ext(name)
	for i := 2; ; i++ {
		if _, err := root.Lstat(final); errors.Is(err, fs.ErrNotExist) {
			break
		} else if i > 99 {
			return "", errOft
		}
		final = strings.TrimSuffix(name, ext) + " (" + strconv.Itoa(i) + ")" + ext
	}
	if who := Uploader(person); who != "" { // before the rename: the importer reads it as it takes the upload
		if err := root.WriteFile("._"+final+".von", []byte(who+"\n"), 0o664); err != nil {
			log.Printf("homes: musik: ._%s.von: %v", final, err)
		}
	}
	return final, root.Rename(part, final)
}

// Hochladen is POST /napi/musik (the app's share target), behind napi's bearer check: each
// multipart "file" part one upload, for the device's person (whoami of its bearer). {"ok":
// true, "dateien": [the names they got]}; a refusal is {"ok": false, "error": a German
// sentence, "dateien": what arrived before it}: 400 not music, 413 too big, 507 disk full.
func Hochladen(w http.ResponseWriter, r *http.Request, dir string, whoami func(bearer string) (string, bool)) {
	http.NewResponseController(w).SetReadDeadline(time.Now().Add(time.Hour)) // a phone's slow uplink; a stalled one lets go
	person, got := "", []string{}
	if b, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && whoami != nil {
		person, _ = whoami(b)
	}
	say := func(code int, err string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]any{"ok": err == "", "dateien": got, "error": err})
	}
	mr, err := r.MultipartReader()
	for err == nil {
		var p *multipart.Part
		if p, err = mr.NextPart(); err != nil || p.FormName() != "file" {
			continue
		}
		name := Name(p.FileName())
		if name == "" {
			say(http.StatusBadRequest, "„"+path.Base(p.FileName())+"“ ist keine Musikdatei (MP3, FLAC, M4A, OGG, Opus, WAV) und keine ZIP.")
			return
		}
		final, err := Ablegen(dir, name, person, p)
		switch {
		case errors.Is(err, errGross):
			say(http.StatusRequestEntityTooLarge, err.Error())
			return
		case errors.Is(err, errLeer), errors.Is(err, errOft):
			say(http.StatusBadRequest, "„"+name+"“: "+err.Error())
			return
		case errors.Is(err, syscall.ENOSPC):
			say(http.StatusInsufficientStorage, "Auf der Box ist kein Platz mehr für Musik.")
			return
		case err != nil:
			log.Printf("homes: musik: %s from the app: %v", name, err)
			say(http.StatusInternalServerError, "„"+name+"“ ließ sich gerade nicht ablegen. Teile es gleich noch einmal.")
			return
		}
		log.Printf("homes: musik: %s from %s's phone", final, person)
		got = append(got, final)
	}
	switch {
	case err != io.EOF:
		say(http.StatusBadRequest, "Das kam nicht ganz als Datei an. Teile die Musik noch einmal.")
	case len(got) == 0:
		say(http.StatusBadRequest, "Es kam keine Musikdatei an.")
	default:
		say(http.StatusOK, "")
	}
}
