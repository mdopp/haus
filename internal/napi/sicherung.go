package napi

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// Daten sichern (haus#207, laib ADR 0029): the Homes app
// (homes-android#194) brings a person's phone backups — WhatsApp's
// msgstore.db.crypt15 and its media, Signal's signal-*.backup, the SMS and
// calls — into archiv-eingang/<person>/<quelle>/<pfad>, where that
// person's archive imports them once its vault is open. A backup is
// several GB and a phone's connection drops, so the upload resumes: POST
// /napi/sicherung/uploads names the file and gets an id and how much the
// box has, HEAD asks again after a cut, PATCH sends the next piece at that
// offset. The person is always the device's (Whoami), never the request's.
//
// A piece is written into <person>/.teil/<id> (hidden, the importer skips
// it; the same file system, so the end is a rename) and fsync'd before it
// is answered: what HEAD says the box has, it has. The last piece renames
// the file into place — replacing one of the same name, as WhatsApp's
// msgstore.db.crypt15 keeps its name every day — and Homes notes it in its
// own ledger (HOMES_DATA/sicherung/<person>/<quelle>.jsonl), which is what
// /fehlend compares against: the importer deletes what it imported, so the
// folder cannot say what arrived.

var (
	sicherungMaxGroesse int64 = 20 << 30 // one file, the contract's 20 GiB
	sicherungMaxStueck  int64 = 16 << 20 // one PATCH body
	sicherungMaxListe         = 5000     // entries of one /fehlend
	sicherungMaxPfad          = 512      // bytes of one pfad
	// what must stay free after the rest of a file: the box lives on this disk too
	sicherungRand uint64 = 1 << 30
	// pieces written at once over all persons; one more is 429, as /napi/upload's
	sicherungConcurrent = 4
	// how long a piece may stall: each read pushes the deadline on, so a
	// slow uplink finishes its 16 MB, a phone gone silent lets go of its slot
	sicherungIdle = 45 * time.Second
	// a part nobody wrote to for this long is given up
	sicherungAlter = 7 * 24 * time.Hour
	// how long a piece waits for the one before it to let go (see claim)
	sicherungUebernahme = 5 * time.Second
)

// Quellen are the folders of a person's inbox, in the order the page shows
// them. sms holds the phone's SMS and call log, which the app encrypts to
// the person's archive (GET /napi/sicherung/schluessel) before it sends
// them: sms-<zeit>.xml.age, anrufe-<zeit>.xml.age.
var Quellen = []string{"whatsapp", "signal", "sms"} // Discord dropped (owner, 2026-11-03)

// teil is the hidden folder of the parts in a person's inbox.
const teil = ".teil"

// What the routes answer, one German sentence each with the next step.
const (
	saysArchivNotSet  = "Das Archiv ist auf dieser Box nicht eingerichtet."
	saysArchivMissing = "Der Eingang des Archivs fehlt auf der Box. Sag dem Admin Bescheid."
	saysNoPerson      = "Dieses Gerät gehört zu keiner Person. Kopple die Homes-App neu, angemeldet mit deinem Namen."
	saysUnreadable    = "Die Anfrage war nicht lesbar. Aktualisiere die Homes-App."
	saysTooMany       = "Zu viele Dateien auf einmal: höchstens 5000 je Anfrage."
	saysTooBig        = "Die Datei ist zu groß: höchstens 20 GB je Datei."
	saysBadSize       = "Die Größe der Datei fehlt oder ist ungültig. Aktualisiere die Homes-App."
	saysNoUpload      = "Diese Übertragung gibt es auf der Box nicht (mehr). Starte sie neu."
	saysBusyFile      = "Diese Datei wird gerade schon übertragen. Frag den Stand ab und mach dort weiter."
	saysBusyBox       = "Die Box nimmt gerade schon andere Sicherungen an. Versuch es gleich noch einmal."
	saysPieceTooBig   = "Ein Stück darf höchstens 16 MB groß sein. Aktualisiere die Homes-App."
	saysMoreThanSaid  = "Es kamen mehr Daten als angekündigt. Starte die Übertragung dieser Datei neu."
	saysContentType   = "Ein Stück kommt als application/offset+octet-stream. Aktualisiere die Homes-App."
	saysNoOffset      = "Es fehlt, ab welchem Byte das Stück gilt (Upload-Offset). Aktualisiere die Homes-App."
	saysNoRoom        = "Auf der Box ist nicht genug Platz für diese Datei. Sag dem Admin Bescheid, dann geht es weiter."
	saysCut           = "Die Übertragung brach ab. Die Homes-App macht beim nächsten Mal dort weiter."
	saysStoreFailed   = "Die Box konnte die Datei nicht speichern. Versuch es später noch einmal; hilft das nicht, sag dem Admin Bescheid."
	saysNoKey         = "Dein Archiv ist noch nicht eingerichtet. Richte es einmal auf der Archiv-Seite ein, dann sichert die Homes-App deine SMS und Anrufe verschlüsselt."
	saysBadKey        = "Der Schlüssel deines Archivs auf der Box ist unlesbar. Sag dem Admin Bescheid."
)

// briefkasten is the file in a person's inbox where their archive leaves
// its public age key (one line, an X25519 recipient), so the phone can
// encrypt to it what only the archive opens.
const briefkasten = ".briefkasten.pub"

// Sicherung is the inbox of the message archives and Homes' ledger of
// what arrived. The pages read its Stand.
type Sicherung struct {
	dir    string // HOMES_ARCHIV_DIR: one folder per person
	ledger string // HOMES_DATA/sicherung: one folder per person, one file per quelle
	slots  chan struct{}
	now    func() time.Time
	intern map[string]ArchivIntern // each person's archive, for the media of a restore (haus#209)

	mu      sync.Mutex
	busy    map[string]*schreiber // person/id being written
	ledgers map[string]*journal   // person/quelle, loaded once
}

// NewSicherung keeps the inbox in dir (HOMES_ARCHIV_DIR) and the ledger
// under dataDir (HOMES_DATA).
func NewSicherung(dir, dataDir string) *Sicherung {
	return &Sicherung{dir: dir, ledger: filepath.Join(dataDir, "sicherung"), slots: make(chan struct{}, sicherungConcurrent),
		now: time.Now, busy: map[string]*schreiber{}, ledgers: map[string]*journal{}}
}

// routes are registered whether or not the box has the inbox: without
// it (s nil) each answers 503 with the sentence the app shows.
func (s *Sicherung) routes(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc, whoami func(string) (string, bool)) {
	as := func(next personHandler) http.HandlerFunc { return auth(s.person(whoami, next)) }
	mux.HandleFunc("GET /napi/sicherung/stand", as(s.stand))
	mux.HandleFunc("POST /napi/sicherung/fehlend", as(s.fehlend))
	mux.HandleFunc("POST /napi/sicherung/uploads", as(s.create))
	mux.HandleFunc("HEAD /napi/sicherung/uploads/{id}", as(s.head))
	mux.HandleFunc("PATCH /napi/sicherung/uploads/{id}", as(s.patch))
	mux.HandleFunc("GET /napi/sicherung/schluessel", as(s.schluessel))
	mux.HandleFunc("GET /napi/sicherung/letzte", as(s.letzteListe))                    // a new phone gets the newest back (haus#209)
	mux.HandleFunc("GET /napi/sicherung/letzte/{quelle}/{pfad...}", as(s.letzteDatei)) // GET also answers HEAD
}

// fehler is a refusal of /napi/sicherung: {"fehler": a German sentence}.
func fehler(w http.ResponseWriter, code int, say string) {
	answer(w, code, map[string]any{"fehler": say})
}

// personHandler is a route that knows the device's person.
type personHandler func(w http.ResponseWriter, r *http.Request, person string)

// person hands a route the device's person: 503 while the box has no
// inbox (s nil, or its folder missing), 403 for a device of nobody.
func (s *Sicherung) person(whoami func(string) (string, bool), next personHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if s == nil {
			fehler(w, http.StatusServiceUnavailable, saysArchivNotSet)
			return
		}
		if fi, err := os.Stat(s.dir); err != nil || !fi.IsDir() {
			log.Printf("homes: sicherung: HOMES_ARCHIV_DIR %s is not a folder: %v", s.dir, err)
			fehler(w, http.StatusServiceUnavailable, saysArchivMissing)
			return
		}
		person := ""
		if bearer, ok := bearerOf(r); ok && whoami != nil {
			person, _ = whoami(bearer)
		}
		if !validPerson(person) {
			fehler(w, http.StatusForbidden, saysNoPerson)
			return
		}
		next(w, r, person)
	}
}

// validPerson is an LLDAP user name that is safe as one folder name.
func validPerson(p string) bool {
	if p == "" || len(p) > 64 || p[0] == '.' {
		return false
	}
	for _, c := range p {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._@-", c)) {
			return false
		}
	}
	return true
}

func validQuelle(q string) bool {
	for _, k := range Quellen {
		if q == k {
			return true
		}
	}
	return false
}

// validPfad is a file inside a quelle: relative, '/'-separated, no
// empty, "..", or dot-leading segment (nothing hidden, so never .teil),
// valid UTF-8 without NUL, at most 512 bytes. os.Root holds the rest.
func validPfad(p string) bool {
	if p == "" || len(p) > sicherungMaxPfad || !utf8.ValidString(p) || strings.ContainsRune(p, 0) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg[0] == '.' {
			return false
		}
	}
	return true
}

// checkFile is the first refusal of a file's quelle, pfad and groesse, or "".
func checkFile(quelle, pfad string, groesse int64) string {
	switch {
	case !validQuelle(quelle):
		return "Unbekannte Quelle „" + quelle + "“: es gibt whatsapp, signal und sms. Aktualisiere die Homes-App."
	case !validPfad(pfad):
		return "Der Pfad „" + pfad + "“ geht nicht: nur Ordner und Dateien unterhalb der Sicherung, nichts Verstecktes (mit Punkt vorn), höchstens 512 Zeichen."
	case groesse < 0:
		return saysBadSize
	case groesse > sicherungMaxGroesse:
		return saysTooBig
	}
	return ""
}

// QuelleStand is what arrived from one quelle: the last file, how many
// files (each name once) and their bytes, and how many wait in the inbox.
type QuelleStand struct {
	Zuletzt *string `json:"zuletzt"` // RFC 3339, UTC; null before the first
	Dateien int     `json:"dateien"`
	Bytes   int64   `json:"bytes"`
	Wartend int     `json:"wartend"`
}

// Stand is a person's whole state, as GET /napi/sicherung/stand says it.
type Stand struct {
	Quellen   map[string]QuelleStand `json:"quellen"`
	FreiBytes int64                  `json:"frei_bytes"`
}

// Stand is person's ledger, what waits in their inbox, and the room left.
func (s *Sicherung) Stand(person string) (Stand, error) {
	st := Stand{Quellen: map[string]QuelleStand{}}
	if !validPerson(person) {
		return st, errors.New("no person")
	}
	var root *os.Root
	if r, err := os.OpenRoot(filepath.Join(s.dir, person)); err == nil {
		root = r
		defer root.Close()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return st, err
	}
	for _, q := range Quellen {
		j, err := s.journal(person, q)
		if err != nil {
			return st, err
		}
		s.mu.Lock()
		qs := QuelleStand{Dateien: len(j.files), Bytes: j.bytes}
		if !j.last.IsZero() {
			t := j.last.UTC().Format(time.RFC3339)
			qs.Zuletzt = &t
		}
		s.mu.Unlock()
		if root != nil {
			qs.Wartend = waiting(root, q)
		}
		st.Quellen[q] = qs
	}
	free, err := freeSpace(s.dir)
	if err != nil {
		return st, err
	}
	st.FreiBytes = int64(min(free, math.MaxInt64))
	return st, nil
}

// waiting counts the whole files in a quelle's folder: what the archive
// has not imported yet. Hidden names are not files of the backup.
func waiting(root *os.Root, quelle string) int {
	n := 0
	fs.WalkDir(root.FS(), quelle, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // the folder not there yet: nothing waits
		}
		if p != quelle && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			n++
		}
		return nil
	})
	return n
}

// GET /napi/sicherung/stand → Stand
func (s *Sicherung) stand(w http.ResponseWriter, r *http.Request, person string) {
	st, err := s.Stand(person)
	if err != nil {
		log.Printf("homes: sicherung: stand of %s: %v", person, err)
		fehler(w, http.StatusInternalServerError, saysStoreFailed)
		return
	}
	answer(w, http.StatusOK, st)
}

// GET /napi/sicherung/schluessel → {empfaenger: "age1…"}, the person's
// archive's public key; 404 before the archive wrote it.
func (s *Sicherung) schluessel(w http.ResponseWriter, r *http.Request, person string) {
	key, err := s.Schluessel(person)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		fehler(w, http.StatusNotFound, saysNoKey)
	case err != nil:
		log.Printf("homes: sicherung: key of %s: %v", person, err)
		fehler(w, http.StatusInternalServerError, saysBadKey)
	default:
		answer(w, http.StatusOK, map[string]any{"empfaenger": key})
	}
}

// Schluessel is the age recipient in person's .briefkasten.pub, read
// through os.Root; fs.ErrNotExist when there is none yet.
func (s *Sicherung) Schluessel(person string) (string, error) {
	if !validPerson(person) {
		return "", errors.New("no person")
	}
	root, err := os.OpenRoot(filepath.Join(s.dir, person))
	if err != nil {
		return "", err
	}
	defer root.Close()
	f, err := root.Open(briefkasten)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s is no file", briefkasten)
	}
	b, err := io.ReadAll(io.LimitReader(f, 1<<10))
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(b))
	if !validRecipient(key) {
		return "", fmt.Errorf("%s holds no age recipient", briefkasten)
	}
	return key, nil
}

// validRecipient is one age X25519 recipient: "age1" and bech32 letters.
func validRecipient(k string) bool {
	if !strings.HasPrefix(k, "age1") || len(k) < 10 || len(k) > 200 {
		return false
	}
	for _, c := range k[4:] {
		if !strings.ContainsRune("qpzry9x8gf2tvdw0s3jn54khce6mua7l", c) {
			return false
		}
	}
	return true
}

// a file the phone has, as /fehlend and /uploads name it
type datei struct {
	Pfad    string `json:"pfad"`
	Groesse int64  `json:"groesse"`
}

// POST /napi/sicherung/fehlend {quelle, dateien:[{pfad, groesse}]} →
// {fehlend:[pfad]}: what the ledger has not received with that size.
func (s *Sicherung) fehlend(w http.ResponseWriter, r *http.Request, person string) {
	var in struct {
		Quelle  string  `json:"quelle"`
		Dateien []datei `json:"dateien"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, int64(sicherungMaxListe)<<12+64<<10)).Decode(&in); err != nil {
		fehler(w, http.StatusBadRequest, saysUnreadable)
		return
	}
	if !validQuelle(in.Quelle) {
		fehler(w, http.StatusBadRequest, checkFile(in.Quelle, "x", 0))
		return
	}
	if len(in.Dateien) > sicherungMaxListe {
		fehler(w, http.StatusBadRequest, saysTooMany)
		return
	}
	for _, d := range in.Dateien {
		if say := checkFile(in.Quelle, d.Pfad, d.Groesse); say != "" {
			fehler(w, http.StatusBadRequest, say)
			return
		}
	}
	j, err := s.journal(person, in.Quelle)
	if err != nil {
		log.Printf("homes: sicherung: ledger of %s/%s: %v", person, in.Quelle, err)
		fehler(w, http.StatusInternalServerError, saysStoreFailed)
		return
	}
	missing := []string{}
	s.mu.Lock()
	for _, d := range in.Dateien {
		if g, ok := j.files[d.Pfad]; !ok || g != d.Groesse {
			missing = append(missing, d.Pfad)
		}
	}
	s.mu.Unlock()
	answer(w, http.StatusOK, map[string]any{"fehlend": missing})
}

// auftrag is one upload, kept beside its part as .teil/<id>.json; Fertig
// once it was put in place, so a phone that lost the last answer hears so.
type auftrag struct {
	Quelle  string `json:"quelle"`
	Pfad    string `json:"pfad"`
	Groesse int64  `json:"groesse"`
	// Geaendert is the file's mtime on the phone (epoch ms), 0 when the app
	// sends none: WhatsApp's msgstore.db.crypt15 keeps its name every day and
	// can keep its size, and only this tells today's file from yesterday's part
	Geaendert int64 `json:"geaendert,omitempty"`
	Fertig    bool  `json:"fertig,omitempty"`
}

// uploadID is the same for the same file, so asking again resumes it; the
// folder it lives in is the person's, so it says nothing to anyone else.
func uploadID(quelle, pfad string, groesse, geaendert int64) string {
	h := sha256.Sum256([]byte(quelle + "\x00" + pfad + "\x00" + strconv.FormatInt(groesse, 10) + "\x00" + strconv.FormatInt(geaendert, 10)))
	return hex.EncodeToString(h[:16])
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil && strings.ToLower(id) == id
}

// open is the person's inbox folder, made on their first upload.
func (s *Sicherung) open(person string) (*os.Root, error) {
	top, err := os.OpenRoot(s.dir)
	if err != nil {
		return nil, err
	}
	defer top.Close()
	if err := top.Mkdir(person, 0o775); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	root, err := top.OpenRoot(person)
	if err != nil {
		return nil, err
	}
	if err := root.Mkdir(teil, 0o775); err != nil && !errors.Is(err, fs.ErrExist) {
		root.Close()
		return nil, err
	}
	return root, nil
}

func readAuftrag(root *os.Root, id string) (auftrag, error) {
	var a auftrag
	b, err := root.ReadFile(teil + "/" + id + ".json")
	if err != nil {
		return a, err
	}
	return a, json.Unmarshal(b, &a)
}

// writeAuftrag replaces .teil/<id>.json whole: written aside, synced, renamed.
func writeAuftrag(root *os.Root, id string, a auftrag) error {
	b, _ := json.Marshal(a)
	tmp := teil + "/" + id + ".json.neu"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o664)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if serr := f.Sync(); err == nil {
		err = serr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		root.Remove(tmp)
		return err
	}
	return root.Rename(tmp, teil+"/"+id+".json")
}

// partSize is how much of an upload the box has.
func partSize(root *os.Root, id string) (int64, error) {
	fi, err := root.Stat(teil + "/" + id)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// schreiber is who holds an upload: stop (nil for a short hold) ends a
// piece still arriving, done closes when it let go.
type schreiber struct {
	stop func()
	done chan struct{}
}

// claim marks person's upload id as held by this request. A phone whose
// connection broke is often still "sending" on the box's side until its
// stall runs out; when it comes back, its new piece (wait > 0) stops the
// old one, which keeps what came, and waits for it to let go — the tus
// way, so a resume is not a 409 for 45 s. False when the id stays held.
func (s *Sicherung) claim(person, id string, stop func(), wait time.Duration) bool {
	key := person + "/" + id
	until := time.Now().Add(wait)
	s.mu.Lock()
	for {
		old := s.busy[key]
		if old == nil {
			s.busy[key] = &schreiber{stop: stop, done: make(chan struct{})}
			s.mu.Unlock()
			return true
		}
		left := time.Until(until)
		if left <= 0 {
			s.mu.Unlock()
			return false
		}
		if old.stop != nil {
			old.stop()
		}
		s.mu.Unlock()
		select {
		case <-old.done:
		case <-time.After(left):
		}
		s.mu.Lock()
	}
}

func (s *Sicherung) release(person, id string) {
	s.mu.Lock()
	if h := s.busy[person+"/"+id]; h != nil {
		close(h.done)
		delete(s.busy, person+"/"+id)
	}
	s.mu.Unlock()
}

// POST /napi/sicherung/uploads {quelle, pfad, groesse, geaendert?} → 201 {id, offset}.
// The same file again, while its part is there, is that part: resume. A
// part already whole (an empty file, a place that failed) is put in place
// here, so a phone that stops at offset == groesse leaves nothing behind.
func (s *Sicherung) create(w http.ResponseWriter, r *http.Request, person string) {
	var in struct {
		Quelle    string `json:"quelle"`
		Pfad      string `json:"pfad"`
		Groesse   *int64 `json:"groesse"`
		Geaendert int64  `json:"geaendert"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&in); err != nil {
		fehler(w, http.StatusBadRequest, saysUnreadable)
		return
	}
	if in.Groesse == nil {
		fehler(w, http.StatusBadRequest, saysBadSize)
		return
	}
	if say := checkFile(in.Quelle, in.Pfad, *in.Groesse); say != "" {
		fehler(w, http.StatusBadRequest, say)
		return
	}
	a := auftrag{Quelle: in.Quelle, Pfad: in.Pfad, Groesse: *in.Groesse, Geaendert: in.Geaendert}
	id := uploadID(a.Quelle, a.Pfad, a.Groesse, a.Geaendert)
	if !s.claim(person, id, nil, 0) { // a piece is still arriving (or hanging): where the box is now
		if root, old, have, ok := s.lookup(person, id); ok {
			root.Close()
			if old.Fertig {
				have = old.Groesse
			}
			answer(w, http.StatusCreated, map[string]any{"id": id, "offset": have})
			return
		}
		fehler(w, http.StatusConflict, saysBusyFile)
		return
	}
	defer s.release(person, id)
	root, err := s.open(person)
	if err != nil {
		log.Printf("homes: sicherung: inbox of %s: %v", person, err)
		fehler(w, http.StatusInternalServerError, saysStoreFailed)
		return
	}
	defer root.Close()
	have, err := partSize(root, id)
	old, aerr := readAuftrag(root, id)
	if err != nil || aerr != nil || old.Fertig { // a new part; one put in place before is sent again
		if err = writeAuftrag(root, id, a); err == nil {
			var f *os.File
			if f, err = root.OpenFile(teil+"/"+id, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o664); err == nil {
				err = f.Close()
			}
		}
		have = 0
	}
	if err == nil && have == a.Groesse {
		err = s.place(root, person, id, a)
	}
	if err != nil {
		log.Printf("homes: sicherung: start %s/%s/%s: %v", person, a.Quelle, a.Pfad, err)
		fehler(w, storeStatus(err), storeSays(err))
		return
	}
	answer(w, http.StatusCreated, map[string]any{"id": id, "offset": have})
}

// HEAD /napi/sicherung/uploads/{id} → 204, Upload-Offset: what the box has.
func (s *Sicherung) head(w http.ResponseWriter, r *http.Request, person string) {
	id := r.PathValue("id")
	root, a, have, ok := s.lookup(person, id)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	root.Close()
	if a.Fertig {
		have = a.Groesse
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(have, 10))
	w.WriteHeader(http.StatusNoContent)
}

// lookup is person's upload id: its folder, its Auftrag and what the box
// has; not ok for an id that is no upload of theirs.
func (s *Sicherung) lookup(person, id string) (*os.Root, auftrag, int64, bool) {
	if !validID(id) {
		return nil, auftrag{}, 0, false
	}
	root, err := os.OpenRoot(filepath.Join(s.dir, person))
	if err != nil {
		return nil, auftrag{}, 0, false
	}
	a, err := readAuftrag(root, id)
	if err != nil {
		root.Close()
		return nil, auftrag{}, 0, false
	}
	have, err := partSize(root, id)
	if err != nil && !a.Fertig {
		root.Close()
		return nil, auftrag{}, 0, false
	}
	return root, a, have, true
}

// conflict is 409 with where the box is.
func conflict(w http.ResponseWriter, say string, have int64) {
	answer(w, http.StatusConflict, map[string]any{"fehler": say, "offset": have})
}

// PATCH /napi/sicherung/uploads/{id}, Upload-Offset: n, the next piece
// → {offset, fertig}. What arrived is kept and synced even when the
// connection breaks mid-piece: HEAD says where to go on.
func (s *Sicherung) patch(w http.ResponseWriter, r *http.Request, person string) {
	id := r.PathValue("id")
	if ct := r.Header.Get("Content-Type"); !strings.EqualFold(strings.TrimSpace(strings.Split(ct, ";")[0]), "application/offset+octet-stream") {
		fehler(w, http.StatusUnsupportedMediaType, saysContentType)
		return
	}
	at, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil || at < 0 {
		fehler(w, http.StatusBadRequest, saysNoOffset)
		return
	}
	if r.ContentLength > sicherungMaxStueck {
		fehler(w, http.StatusRequestEntityTooLarge, saysPieceTooBig)
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		fehler(w, http.StatusTooManyRequests, saysBusyBox)
		return
	}
	if !validID(id) {
		fehler(w, http.StatusNotFound, saysNoUpload)
		return
	}
	body := &idleReader{r: r.Body, rc: http.NewResponseController(w)}
	if !s.claim(person, id, body.stop, sicherungUebernahme) {
		have := int64(0)
		if root, a, h, ok := s.lookup(person, id); ok {
			root.Close()
			have = h
			if a.Fertig {
				have = a.Groesse
			}
		}
		conflict(w, saysBusyFile, have)
		return
	}
	defer s.release(person, id)
	root, a, have, ok := s.lookup(person, id)
	if !ok {
		fehler(w, http.StatusNotFound, saysNoUpload)
		return
	}
	defer root.Close()
	if a.Fertig { // the last answer got lost: it is in place
		if at != a.Groesse || r.ContentLength > 0 {
			conflict(w, "Diese Datei ist schon vollständig auf der Box.", a.Groesse)
			return
		}
		answer(w, http.StatusOK, map[string]any{"offset": a.Groesse, "fertig": true})
		return
	}
	if at != have {
		conflict(w, "Die Box hat von dieser Datei "+strconv.FormatInt(have, 10)+" Bytes; mach dort weiter.", have)
		return
	}
	rest := a.Groesse - have
	if r.ContentLength > rest {
		fehler(w, http.StatusBadRequest, saysMoreThanSaid)
		return
	}
	if rest > 0 {
		if free, err := freeSpace(s.dir); err != nil {
			log.Printf("homes: sicherung: free space of %s: %v", s.dir, err)
		} else if free < uint64(rest)+sicherungRand {
			log.Printf("homes: sicherung: %s has %d bytes free, %s/%s wants %d more", s.dir, free, person, a.Pfad, uint64(rest)+sicherungRand)
			fehler(w, http.StatusInsufficientStorage, saysNoRoom)
			return
		}
	}
	written, code, say := s.write(root, id, have, min(rest, sicherungMaxStueck), rest, body)
	now := have + written
	if code != 0 {
		log.Printf("homes: sicherung: %s/%s/%s at %d: %s", person, a.Quelle, a.Pfad, now, say)
		answer(w, code, map[string]any{"fehler": say, "offset": now})
		return
	}
	fertig := now == a.Groesse
	if fertig {
		if err := s.place(root, person, id, a); err != nil {
			log.Printf("homes: sicherung: place %s/%s/%s: %v", person, a.Quelle, a.Pfad, err)
			fehler(w, storeStatus(err), storeSays(err))
			return
		}
	}
	answer(w, http.StatusOK, map[string]any{"offset": now, "fertig": fertig})
}

// idleReader moves the read deadline on before each read: a stall, not
// a slow phone, ends a piece; stop ends it now (a newer piece took over).
// Not every ResponseWriter has deadlines (a test recorder); the server's has.
type idleReader struct {
	r       io.Reader
	rc      *http.ResponseController
	mu      sync.Mutex
	stopped bool
}

var errTakenOver = errors.New("a newer piece took over")

func (i *idleReader) Read(p []byte) (int, error) {
	i.mu.Lock()
	if i.stopped {
		i.mu.Unlock()
		return 0, errTakenOver
	}
	i.rc.SetReadDeadline(time.Now().Add(sicherungIdle))
	i.mu.Unlock()
	return i.r.Read(p)
}

func (i *idleReader) stop() {
	i.mu.Lock()
	i.stopped = true
	i.rc.SetReadDeadline(time.Now())
	i.mu.Unlock()
}

// write appends at most limit bytes of body to the part at have, synced.
// A body longer than the piece or the file is refused whole (the part is
// cut back to have); a body that breaks off keeps what came.
func (s *Sicherung) write(root *os.Root, id string, have, limit, rest int64, body io.Reader) (int64, int, string) {
	f, err := root.OpenFile(teil+"/"+id, os.O_WRONLY, 0)
	if err != nil {
		log.Printf("homes: sicherung: open part %s: %v", id, err)
		return 0, http.StatusInternalServerError, saysStoreFailed
	}
	defer f.Close()
	if _, err := f.Seek(have, io.SeekStart); err != nil {
		log.Printf("homes: sicherung: seek part %s: %v", id, err)
		return 0, http.StatusInternalServerError, saysStoreFailed
	}
	n, err := io.Copy(diskWriter{f}, io.LimitReader(body, limit))
	if err == nil {
		var one [1]byte
		if m, _ := io.ReadFull(body, one[:]); m > 0 { // more than the piece may be
			f.Truncate(have)
			f.Sync()
			if limit == rest {
				return 0, http.StatusBadRequest, saysMoreThanSaid
			}
			return 0, http.StatusRequestEntityTooLarge, saysPieceTooBig
		}
	}
	if serr := f.Sync(); err == nil && serr != nil {
		err = &diskErr{serr}
	}
	var de *diskErr
	switch {
	case err == nil:
		return n, 0, ""
	case errors.As(err, &de):
		log.Printf("homes: sicherung: write part %s: %v", id, err)
		return n, storeStatus(err), storeSays(err)
	default: // the phone's side: a cut, a timeout
		log.Printf("homes: sicherung: part %s: %d bytes, then %v", id, n, err)
		return n, http.StatusRequestTimeout, saysCut
	}
}

// place puts a whole part where the importer looks: <quelle>/<pfad>, with
// the phone's mtime. A file of that name still waiting there is kept beside
// it as <pfad>.<its mtime>, never replaced: WhatsApp's msgstore.db.crypt15
// keeps its name daily, and yesterday's may hold a chat deleted since, which
// the person's Archiv has not imported yet (its Tresor was closed).
func (s *Sicherung) place(root *os.Root, person, id string, a auftrag) error {
	f, err := root.OpenFile(teil+"/"+id, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	err = f.Sync()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return &diskErr{err}
	}
	to := a.Quelle + "/" + a.Pfad
	if err := root.MkdirAll(path.Dir(to), 0o775); err != nil {
		return err
	}
	if old, err := root.Lstat(to); err == nil && old.Mode().IsRegular() {
		if err := keepBeside(root, to, old.ModTime()); err != nil {
			return err
		}
	}
	if err := root.Rename(teil+"/"+id, to); err != nil {
		return err
	}
	if a.Geaendert > 0 {
		t := time.UnixMilli(a.Geaendert)
		if err := root.Chtimes(to, t, t); err != nil {
			log.Printf("homes: sicherung: mtime of %s/%s: %v", person, to, err)
		}
	}
	if d, err := root.Open(path.Dir(to)); err == nil { // the rename itself, durable
		d.Sync()
		d.Close()
	}
	if err := s.note(person, a); err != nil {
		log.Printf("homes: sicherung: ledger of %s/%s: %v (%s is in place)", person, a.Quelle, err, a.Pfad)
	}
	a.Fertig = true
	if err := writeAuftrag(root, id, a); err != nil {
		root.Remove(teil + "/" + id + ".json")
	}
	log.Printf("homes: sicherung: %s/%s/%s arrived, %d bytes", person, a.Quelle, a.Pfad, a.Groesse)
	return nil
}

// keepBeside moves the waiting file at to aside as to.<mtime>, the first
// free one of to.20261101-020000, to.20261101-020000-2, …
func keepBeside(root *os.Root, to string, at time.Time) error {
	base := to + "." + at.UTC().Format("20060102-150405")
	for i := 1; i < 100; i++ {
		name := base
		if i > 1 {
			name = base + "-" + strconv.Itoa(i)
		}
		if _, err := root.Lstat(name); errors.Is(err, fs.ErrNotExist) {
			return root.Rename(to, name)
		}
	}
	return fmt.Errorf("no free name beside %s", to)
}

func storeStatus(err error) int {
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
		return http.StatusInsufficientStorage
	}
	return http.StatusInternalServerError
}

func storeSays(err error) string {
	if storeStatus(err) == http.StatusInsufficientStorage {
		return saysNoRoom
	}
	return saysStoreFailed
}

// journal is one person's ledger of one quelle: each pfad with the size it
// last arrived with, when the last file came, and their bytes.
type journal struct {
	file  string
	files map[string]int64
	last  time.Time
	bytes int64
}

// a ledger line
type eintrag struct {
	Pfad    string    `json:"pfad"`
	Groesse int64     `json:"groesse"`
	Zeit    time.Time `json:"zeit"`
}

func (j *journal) add(e eintrag) {
	if old, ok := j.files[e.Pfad]; ok {
		j.bytes -= old
	}
	j.files[e.Pfad] = e.Groesse
	j.bytes += e.Groesse
	if e.Zeit.After(j.last) {
		j.last = e.Zeit
	}
}

// journal is person's ledger of quelle, read from disk the first time.
func (s *Sicherung) journal(person, quelle string) (*journal, error) {
	key := person + "/" + quelle
	s.mu.Lock()
	defer s.mu.Unlock()
	if j := s.ledgers[key]; j != nil {
		return j, nil
	}
	j := &journal{file: filepath.Join(s.ledger, person, quelle+".jsonl"), files: map[string]int64{}}
	f, err := os.Open(j.file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 64<<10)
		for sc.Scan() {
			var e eintrag
			if json.Unmarshal(sc.Bytes(), &e) == nil && e.Pfad != "" {
				j.add(e)
			}
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("%s: %w", j.file, err)
		}
	}
	s.ledgers[key] = j
	return j, nil
}

// note appends one arrival to the ledger, synced.
func (s *Sicherung) note(person string, a auftrag) error {
	j, err := s.journal(person, a.Quelle)
	if err != nil {
		return err
	}
	e := eintrag{Pfad: a.Pfad, Groesse: a.Groesse, Zeit: s.now().UTC()}
	line, _ := json.Marshal(e)
	s.mu.Lock()
	defer s.mu.Unlock()
	j.add(e) // what arrived, arrived, even when the line cannot be written
	if err := os.MkdirAll(filepath.Dir(j.file), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(j.file, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if serr := f.Sync(); err == nil {
		err = serr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Aufraeumen removes the parts nobody wrote to for seven days, and the
// notes of uploads put in place as long ago, in every person's inbox.
func (s *Sicherung) Aufraeumen() error {
	es, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range es {
		if !e.IsDir() || !validPerson(e.Name()) {
			continue
		}
		s.aufraeumen(e.Name())
	}
	return nil
}

func (s *Sicherung) aufraeumen(person string) {
	root, err := os.OpenRoot(filepath.Join(s.dir, person))
	if err != nil {
		return
	}
	defer root.Close()
	d, err := root.Open(teil)
	if err != nil {
		return
	}
	names, _ := d.Readdirnames(-1)
	d.Close()
	ids := map[string]bool{}
	for _, n := range names {
		id, _, _ := strings.Cut(n, ".")
		if validID(id) {
			ids[id] = true
		}
	}
	cut := s.now().Add(-sicherungAlter)
	for id := range ids {
		if !s.claim(person, id, nil, 0) {
			continue // being written right now
		}
		touched := time.Time{}
		for _, n := range []string{id, id + ".json", id + ".json.neu"} {
			if fi, err := root.Stat(teil + "/" + n); err == nil && fi.ModTime().After(touched) {
				touched = fi.ModTime()
			}
		}
		if touched.Before(cut) {
			for _, n := range []string{id, id + ".json", id + ".json.neu"} {
				root.Remove(teil + "/" + n)
			}
			log.Printf("homes: sicherung: gave up %s's part %s, untouched since %s", person, id, touched.Format(time.DateOnly))
		}
		s.release(person, id)
	}
}

// Pflegen tidies the inbox now and then daily, until stop closes.
func (s *Sicherung) Pflegen(stop <-chan struct{}) {
	for {
		if err := s.Aufraeumen(); err != nil {
			log.Printf("homes: sicherung: tidying %s: %v", s.dir, err)
		}
		select {
		case <-stop:
			return
		case <-time.After(24 * time.Hour):
		}
	}
}
