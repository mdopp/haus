package napi

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Sicherung zurückholen (haus#209, laib ADR 0029 amendment 2026-11-01 §3–4):
// a person's archive keeps their newest raw backup per source in
// archiv-eingang/<person>/letzte/ — Signal's signal-*.backup and WhatsApp's
// Databases/msgstore.db.crypt15, both encrypted by the messenger, and
// WhatsApp's media age-encrypted to the person's archive (Media/<pfad>.age).
// A new phone of the same person (the device's Whoami, never the request's)
// gets them back:
//
//	GET /napi/sicherung/letzte                  {quellen: {signal: [{pfad, groesse, geaendert}], whatsapp: […]}}
//	GET /napi/sicherung/letzte/<quelle>/<pfad>  the file, with Range (a backup is several GB, a phone's line drops)
//
// Signal and the WhatsApp database come straight from letzte/. The media
// only the open archive can decrypt: Haus asks the person's archive
// (/intern/whatsapp/medien/<pfad>, ARCHIV_HAUS_SECRET in X-Archiv-Haus) and
// passes its answer on; a closed vault is 423 with the sentence the app
// shows. Another person's device asks its own letzte/, so their file is 404.

// letzte is the archive's folder of the newest backups in a person's inbox
// (archiv's Briefkasten.Letzte, /eingang/letzte inside the archive).
const letzte = "letzte"

// letzteQuellen are the sources a phone can get back, in the order the
// page shows them.
var letzteQuellen = []string{"signal", "whatsapp"}

// medienPrefix is where WhatsApp's media lie in letzte/whatsapp/, each as
// <pfad>.age; the phone names them without .age.
const medienPrefix = "Media/"

const (
	saysLetzteNichtDa   = "Diese Sicherung gibt es auf der Box nicht (mehr). Lade die Liste neu."
	saysMedienZu        = "Öffne dein Archiv, dann kommen auch die Fotos und Sprachnachrichten zurück."
	saysMedienNichtHier = "Die Fotos und Sprachnachrichten lassen sich auf dieser Box noch nicht zurückholen. Sag dem Admin Bescheid."
	saysArchivSchweigt  = "Dein Archiv antwortet gerade nicht. Versuch es gleich noch einmal; hilft das nicht, sag dem Admin Bescheid."
)

// ArchivIntern is the way into one person's archive: its address as Haus
// reaches it and the secret it wants in X-Archiv-Haus.
type ArchivIntern struct {
	URL    string
	Secret string
}

// ArchivInternFromEnv reads HAUS_ARCHIV_INTERN, "<person>=<url>" separated
// by spaces (each person's archive instance as this pod reaches it), and
// each person's secret from HAUS_ARCHIV_SECRET_<PERSON> — laib's suffix
// for a per-person secret: upper case, '-' as '_'. A person without both
// has no media to give back.
func ArchivInternFromEnv(getenv func(string) string) map[string]ArchivIntern {
	out := map[string]ArchivIntern{}
	for _, f := range strings.Fields(getenv("HAUS_ARCHIV_INTERN")) {
		person, u, ok := strings.Cut(f, "=")
		if !ok || !validPerson(person) {
			log.Printf("haus: HAUS_ARCHIV_INTERN: %q is no <person>=<url>", f)
			continue
		}
		if pu, err := url.Parse(u); err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
			log.Printf("haus: HAUS_ARCHIV_INTERN: %q is no http(s) address for %s", u, person)
			continue
		}
		secret := getenv("HAUS_ARCHIV_SECRET_" + strings.ToUpper(strings.ReplaceAll(person, "-", "_")))
		if secret == "" {
			log.Printf("haus: HAUS_ARCHIV_INTERN: no secret for %s: their media cannot come back", person)
			continue
		}
		out[person] = ArchivIntern{URL: strings.TrimRight(u, "/"), Secret: secret}
	}
	return out
}

// SetArchive gives Haus the way into each person's archive, for the media.
func (s *Sicherung) SetArchive(intern map[string]ArchivIntern) {
	s.intern = intern
}

// archivClient fetches a medium from an archive: no overall timeout (a
// video may take minutes on a slow phone line), but an archive that does
// not answer is let go of.
var archivClient = &http.Client{Transport: &http.Transport{
	DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
	ResponseHeaderTimeout: 30 * time.Second,
}}

// LetzteDatei is one file a new phone can get back.
type LetzteDatei struct {
	Pfad      string `json:"pfad"`
	Groesse   int64  `json:"groesse"`
	Geaendert int64  `json:"geaendert"` // epoch ms, as the upload's
}

// Letzte lists what lies in person's letzte/, per source; a source with
// nothing is an empty list. The media's groesse is their plaintext's.
func (s *Sicherung) Letzte(person string) (map[string][]LetzteDatei, error) {
	out := map[string][]LetzteDatei{}
	for _, q := range letzteQuellen {
		out[q] = []LetzteDatei{}
	}
	if !validPerson(person) {
		return out, errors.New("no person")
	}
	root, err := os.OpenRoot(filepath.Join(s.dir, person))
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	} else if err != nil {
		return out, err
	}
	defer root.Close()
	for _, q := range letzteQuellen {
		dir := letzte + "/" + q
		err := fs.WalkDir(root.FS(), dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if p == dir {
					return fs.SkipDir // nothing kept for this source yet
				}
				return err
			}
			if p != dir && strings.HasPrefix(d.Name(), ".") { // the archive's temporary names
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			rel := strings.TrimPrefix(p, dir+"/")
			fi, err := d.Info()
			if err != nil {
				return nil // gone meanwhile
			}
			e := LetzteDatei{Pfad: rel, Groesse: fi.Size(), Geaendert: fi.ModTime().UnixMilli()}
			if q == "whatsapp" && strings.HasPrefix(rel, medienPrefix) {
				if !strings.HasSuffix(rel, ".age") {
					return nil // only the encrypted media are the archive's
				}
				e.Pfad = strings.TrimSuffix(rel, ".age")
				f, err := root.Open(p)
				if err != nil {
					return nil
				}
				n, err := ageKlarGroesse(f, fi.Size())
				f.Close()
				if err != nil {
					log.Printf("haus: sicherung: letzte of %s: %s: %v", person, rel, err)
					return nil
				}
				e.Groesse = n
			}
			if validPfad(e.Pfad) {
				out[q] = append(out[q], e)
			}
			return nil
		})
		if err != nil {
			return out, err
		}
		sort.Slice(out[q], func(i, j int) bool { return out[q][i].Pfad < out[q][j].Pfad })
	}
	return out, nil
}

// ageKlarGroesse is the plaintext length of an age file of size bytes:
// the header ends after its "--- " line, then a 16-byte nonce, then
// chunks of 64 KiB, each with a 16-byte tag (the last one shorter, never
// empty unless the file is). Read without the key: the header only.
func ageKlarGroesse(r io.Reader, size int64) (int64, error) {
	br := bufio.NewReaderSize(r, 4096)
	var hdr int64
	for i := 0; ; i++ {
		line, err := br.ReadSlice('\n')
		hdr += int64(len(line))
		if err != nil || i > 64 {
			return 0, errors.New("no age header")
		}
		if i == 0 && !bytes.HasPrefix(line, []byte("age-encryption.org/v1")) {
			return 0, errors.New("no age file")
		}
		if bytes.HasPrefix(line, []byte("--- ")) {
			break
		}
	}
	const nonce, chunk, tag = 16, 64 << 10, 16
	payload := size - hdr - nonce
	if payload < tag {
		return 0, errors.New("age file cut short")
	}
	chunks := (payload + chunk + tag - 1) / (chunk + tag)
	return payload - chunks*tag, nil
}

// GET /napi/sicherung/letzte → {quellen: {signal: […], whatsapp: […]}}
func (s *Sicherung) letzteListe(w http.ResponseWriter, r *http.Request, person string) {
	q, err := s.Letzte(person)
	if err != nil {
		log.Printf("haus: sicherung: letzte of %s: %v", person, err)
		fehler(w, http.StatusInternalServerError, saysStoreFailed)
		return
	}
	answer(w, http.StatusOK, map[string]any{"quellen": q})
}

// GET|HEAD /napi/sicherung/letzte/<quelle>/<pfad> → the file, with Range.
// Its ETag changes with the file, so a phone resuming with If-Range gets
// the whole new file instead of a mix when the backup was replaced
// (WhatsApp's database keeps its name every night).
func (s *Sicherung) letzteDatei(w http.ResponseWriter, r *http.Request, person string) {
	quelle, pfad := r.PathValue("quelle"), r.PathValue("pfad")
	if (quelle != "signal" && quelle != "whatsapp") || !validPfad(pfad) {
		fehler(w, http.StatusNotFound, saysLetzteNichtDa)
		return
	}
	if quelle == "whatsapp" && strings.HasPrefix(pfad, medienPrefix) {
		s.medium(w, r, person, strings.TrimPrefix(pfad, medienPrefix))
		return
	}
	root, err := os.OpenRoot(filepath.Join(s.dir, person, letzte, quelle))
	if err != nil {
		fehler(w, http.StatusNotFound, saysLetzteNichtDa)
		return
	}
	defer root.Close()
	f, err := root.Open(filepath.FromSlash(pfad))
	if err != nil {
		fehler(w, http.StatusNotFound, saysLetzteNichtDa)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		fehler(w, http.StatusNotFound, saysLetzteNichtDa)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", `"`+strconv.FormatInt(fi.Size(), 36)+"-"+strconv.FormatInt(fi.ModTime().UnixNano(), 36)+`"`)
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

// medium passes one WhatsApp medium on from the person's open archive,
// Range and all; a closed vault is 423 with the sentence the app shows.
func (s *Sicherung) medium(w http.ResponseWriter, r *http.Request, person, rel string) {
	a, ok := s.intern[person]
	if !ok {
		fehler(w, http.StatusServiceUnavailable, saysMedienNichtHier)
		return
	}
	segs := strings.Split(rel, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, a.URL+"/intern/whatsapp/medien/"+strings.Join(segs, "/"), nil)
	if err != nil {
		fehler(w, http.StatusNotFound, saysLetzteNichtDa)
		return
	}
	req.Header.Set("X-Archiv-Haus", a.Secret)
	for _, h := range []string{"Range", "If-Range"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	resp, err := archivClient.Do(req)
	if err != nil {
		log.Printf("haus: sicherung: archive of %s: %v", person, err)
		fehler(w, http.StatusServiceUnavailable, saysArchivSchweigt)
		return
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusPartialContent, http.StatusRequestedRangeNotSatisfiable:
		for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
			if v := resp.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		if r.Method != http.MethodHead {
			io.Copy(w, resp.Body)
		}
	case http.StatusLocked:
		fehler(w, http.StatusLocked, saysMedienZu)
	case http.StatusNotFound:
		fehler(w, http.StatusNotFound, saysLetzteNichtDa)
	default:
		log.Printf("haus: sicherung: archive of %s answered %d for a medium", person, resp.StatusCode)
		fehler(w, http.StatusBadGateway, saysArchivSchweigt)
	}
}
