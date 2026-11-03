package napi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mdopp/homes/internal/reitti"
)

// Standortverlauf übernehmen (owner 2026-11-03): a person shares an old
// location history to the Homes app — Google Maps' Timeline.json, a
// Takeout Records.json, a GPX or GeoJSON file — and Homes brings it into
// their Reitti with their own token, no browser and no Reitti login. The
// person is the device's (Whoami), never the request's, and so is the
// token.
//
// A Google export is hundreds of MB and a phone's connection drops, so the
// file comes the way a backup does (sicherung.go): POST names it and gets
// an id and how much the box has, HEAD asks again after a cut, PATCH sends
// the next piece. The last piece starts the import: Homes reads the file
// point by point (reitti.Lesen) and hands Reitti batches of them
// (reitti.Uebernehmen); GET on the id says how far it got and, at the end,
// what came over in one German sentence. The file is deleted when the
// import ends, whatever the end; a state is forgotten after seven days.
//
// HOMES_DATA/standort-import/<person>/<id>.teil is the file, <id>.json its
// state. A Homes restarted mid-import goes on when the phone asks next
// (GET): Reitti keeps a point it has once, so the batches sent before add
// nothing twice.

var (
	importMaxGroesse int64  = 5 << 30  // one file; Reitti's own upload limit
	importMaxStueck  int64  = 16 << 20 // one PATCH body, as a backup's
	importStapel            = 25000    // points in one request to Reitti
	importRand       uint64 = 1 << 30  // what stays free after the rest of a file
	importAlter             = 7 * 24 * time.Hour
	importIdle              = 45 * time.Second
	importVersuche          = 3                // tries of one batch while Reitti does not answer
	importPause             = 20 * time.Second // between them
	importStandAlle         = 2 * time.Second  // how often a running import writes its state
)

const (
	standHochladen = "hochladen" // the file is arriving
	standLaeuft    = "laeuft"    // Homes reads it and hands it to Reitti
	standFertig    = "fertig"
	standFehler    = "fehler"
)

const (
	saysImportNoToken   = "Verbinde zuerst Reitti: öffne in Homes Mehr → Standortverlauf und füge dort einmal deinen Reitti-Schlüssel ein. Dann teile die Datei noch einmal."
	saysImportEmpty     = "Die Datei ist leer."
	saysImportTooBig    = "Die Datei ist zu groß: höchstens 5 GB."
	saysImportName      = "Der Dateiname fehlt oder ist zu lang. Aktualisiere die Homes-App."
	saysImportUnknown   = "Diesen Import gibt es auf der Box nicht (mehr). Teile die Datei noch einmal."
	saysImportFormat    = "Das ist kein Standortverlauf, den Homes kennt. Es gehen: die Zeitachse aus Google Maps (Timeline.json), Google Takeout (Records.json), GPX und GeoJSON."
	saysImportNoPoints  = "In der Datei stehen keine Orte mit Zeit, die Reitti übernehmen kann."
	saysImportStoreFail = "Die Box konnte die Datei nicht speichern. Versuch es später noch einmal; hilft das nicht, sag dem Admin Bescheid."
)

// importStand is a file's import as Homes keeps it and GET answers it.
type importStand struct {
	Name      string     `json:"name"`
	Groesse   int64      `json:"groesse"`
	Stand     string     `json:"stand"`
	Format    string     `json:"format,omitempty"`
	Gelesen   int64      `json:"gelesen"` // bytes of the file read so far
	Punkte    int        `json:"punkte"`  // points Reitti took
	Besuche   int        `json:"besuche"` // of them, stays at a place (two points each)
	Von       *time.Time `json:"von,omitempty"`
	Bis       *time.Time `json:"bis,omitempty"`
	Text      string     `json:"text,omitempty"`   // the end, one German sentence
	Fehler    string     `json:"fehler,omitempty"` // why it stopped, one German sentence
	Grund     string     `json:"grund,omitempty"`  // and as a word the app reads
	Geaendert time.Time  `json:"geaendert"`
}

// importer holds the persons' imports.
type importer struct {
	r     *reitti.Reitti
	dir   string // HOMES_DATA/standort-import
	now   func() time.Time
	slots chan struct{} // pieces written at once
	lauf  chan struct{} // imports run one at a time: Reitti is one box's

	mu      sync.Mutex
	busy    map[string]*idleReader // person/id: a piece arriving
	laufend map[string]bool        // person/id: an import running
}

func newImporter(r *reitti.Reitti) *importer {
	return &importer{r: r, dir: filepath.Join(r.Dir, "standort-import"), now: time.Now,
		slots: make(chan struct{}, 2), lauf: make(chan struct{}, 1), busy: map[string]*idleReader{}, laufend: map[string]bool{}}
}

func (s *standort) importRoutes(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc) {
	im := func(next func(*importer, http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
		return auth(s.person(func(w http.ResponseWriter, r *http.Request, person string) { next(s.importer(), w, r, person) }))
	}
	mux.HandleFunc("POST /napi/standort/import", im((*importer).create))
	mux.HandleFunc("HEAD /napi/standort/import/{id}", im((*importer).head))
	mux.HandleFunc("PATCH /napi/standort/import/{id}", im((*importer).patch))
	mux.HandleFunc("GET /napi/standort/import/{id}", im((*importer).stand))
}

func (s *standort) importer() *importer {
	s.once.Do(func() { s.imp = newImporter(s.r) })
	return s.imp
}

func importID(person, name string, groesse int64) string {
	h := sha256.Sum256([]byte(person + "\x00" + name + "\x00" + strconv.FormatInt(groesse, 10)))
	return hex.EncodeToString(h[:16])
}

func (im *importer) folder(person string) string { return filepath.Join(im.dir, person) }
func (im *importer) teil(person, id string) string {
	return filepath.Join(im.folder(person), id+".teil")
}

func (im *importer) read(person, id string) (importStand, error) {
	var st importStand
	if !validID(id) {
		return st, os.ErrNotExist
	}
	b, err := os.ReadFile(filepath.Join(im.folder(person), id+".json"))
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(b, &st)
}

func (im *importer) write(person, id string, st importStand) error {
	st.Geaendert = im.now().UTC()
	b, _ := json.Marshal(st)
	p := filepath.Join(im.folder(person), id+".json")
	if err := os.WriteFile(p+".neu", b, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".neu", p)
}

func partLen(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return -1
	}
	return fi.Size()
}

// sweep forgets person's imports untouched for importAlter, file and state.
func (im *importer) sweep(person string) {
	es, _ := os.ReadDir(im.folder(person))
	for _, e := range es {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !validID(id) {
			continue
		}
		st, err := im.read(person, id)
		if err == nil && im.now().Sub(st.Geaendert) < importAlter {
			continue
		}
		im.mu.Lock()
		running := im.laufend[person+"/"+id] || im.busy[person+"/"+id] != nil
		im.mu.Unlock()
		if !running {
			os.Remove(im.teil(person, id))
			os.Remove(filepath.Join(im.folder(person), e.Name()))
		}
	}
}

// POST /napi/standort/import {name, groesse} → 201 {id, offset}. The same
// file again while it arrives is that upload: resume. While its import
// runs, offset is the whole file (the app asks GET); after it ended the
// file comes anew.
func (im *importer) create(w http.ResponseWriter, r *http.Request, person string) {
	var in struct {
		Name    string `json:"name"`
		Groesse *int64 `json:"groesse"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&in); err != nil || in.Groesse == nil {
		fehler(w, http.StatusBadRequest, saysUnreadable)
		return
	}
	name := strings.TrimSpace(in.Name)
	switch {
	case name == "" || len(name) > 255 || !utf8.ValidString(name) || strings.ContainsAny(name, "\x00/"):
		fehler(w, http.StatusBadRequest, saysImportName)
		return
	case *in.Groesse <= 0:
		fehler(w, http.StatusBadRequest, saysImportEmpty)
		return
	case *in.Groesse > importMaxGroesse:
		fehler(w, http.StatusRequestEntityTooLarge, saysImportTooBig)
		return
	}
	if _, ok := im.r.Konto(person); !ok {
		standortFehler(w, http.StatusConflict, grundKeinSchluessel, saysImportNoToken)
		return
	}
	if err := os.MkdirAll(im.folder(person), 0o700); err != nil {
		log.Printf("homes: standort-import: folder of %s: %v", person, err)
		fehler(w, http.StatusInternalServerError, saysImportStoreFail)
		return
	}
	im.sweep(person)
	id := importID(person, name, *in.Groesse)
	key := person + "/" + id
	im.mu.Lock()
	defer im.mu.Unlock()
	st, err := im.read(person, id)
	have := partLen(im.teil(person, id))
	switch {
	case err == nil && st.Stand == standLaeuft:
		if !im.laufend[key] {
			im.start(person, id)
		}
		answer(w, http.StatusCreated, map[string]any{"id": id, "offset": st.Groesse})
		return
	case err == nil && st.Stand == standHochladen && have >= 0:
		answer(w, http.StatusCreated, map[string]any{"id": id, "offset": have})
		return
	case im.busy[key] != nil:
		fehler(w, http.StatusConflict, saysBusyFile)
		return
	}
	if free, ferr := freeSpace(im.dir); ferr == nil && free < uint64(*in.Groesse)+importRand {
		log.Printf("homes: standort-import: %s has %d bytes free, %s wants %d", im.dir, free, person, *in.Groesse)
		fehler(w, http.StatusInsufficientStorage, saysNoRoom)
		return
	}
	st = importStand{Name: name, Groesse: *in.Groesse, Stand: standHochladen}
	err = os.WriteFile(im.teil(person, id), nil, 0o600)
	if err == nil {
		err = im.write(person, id, st)
	}
	if err != nil {
		log.Printf("homes: standort-import: start %s: %v", key, err)
		fehler(w, http.StatusInternalServerError, saysImportStoreFail)
		return
	}
	answer(w, http.StatusCreated, map[string]any{"id": id, "offset": 0})
}

// HEAD /napi/standort/import/{id} → 204, Upload-Offset.
func (im *importer) head(w http.ResponseWriter, r *http.Request, person string) {
	id := r.PathValue("id")
	st, err := im.read(person, id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	have := st.Groesse
	if st.Stand == standHochladen {
		if have = partLen(im.teil(person, id)); have < 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(have, 10))
	w.WriteHeader(http.StatusNoContent)
}

// PATCH /napi/standort/import/{id}, Upload-Offset: n → {offset, fertig}.
// The piece that makes the file whole starts its import.
func (im *importer) patch(w http.ResponseWriter, r *http.Request, person string) {
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
	if r.ContentLength > importMaxStueck {
		fehler(w, http.StatusRequestEntityTooLarge, saysPieceTooBig)
		return
	}
	select {
	case im.slots <- struct{}{}:
		defer func() { <-im.slots }()
	default:
		fehler(w, http.StatusTooManyRequests, saysBusyBox)
		return
	}
	key := person + "/" + id
	body := &idleReader{r: r.Body, rc: http.NewResponseController(w), idle: importIdle}
	if !im.claim(key, body) {
		conflict(w, saysBusyFile, max(partLen(im.teil(person, id)), 0))
		return
	}
	defer im.release(key, body)
	st, err := im.read(person, id)
	if err != nil {
		fehler(w, http.StatusNotFound, saysImportUnknown)
		return
	}
	if st.Stand != standHochladen { // the last answer got lost: it is whole
		if at != st.Groesse || r.ContentLength > 0 {
			conflict(w, "Diese Datei ist schon vollständig auf der Box.", st.Groesse)
			return
		}
		answer(w, http.StatusOK, map[string]any{"offset": st.Groesse, "fertig": true})
		return
	}
	have := partLen(im.teil(person, id))
	if have < 0 {
		fehler(w, http.StatusNotFound, saysImportUnknown)
		return
	}
	if at != have {
		conflict(w, "Die Box hat von dieser Datei "+strconv.FormatInt(have, 10)+" Bytes; mach dort weiter.", have)
		return
	}
	rest := st.Groesse - have
	if r.ContentLength > rest {
		fehler(w, http.StatusBadRequest, saysMoreThanSaid)
		return
	}
	written, code, say := im.append(person, id, have, min(rest, importMaxStueck), rest, body)
	now := have + written
	if code != 0 {
		log.Printf("homes: standort-import: %s at %d: %s", key, now, say)
		answer(w, code, map[string]any{"fehler": say, "offset": now})
		return
	}
	fertig := now == st.Groesse
	if fertig {
		st.Stand = standLaeuft
		if err := im.write(person, id, st); err != nil {
			log.Printf("homes: standort-import: %s: %v", key, err)
			fehler(w, http.StatusInternalServerError, saysImportStoreFail)
			return
		}
		im.mu.Lock()
		im.start(person, id)
		im.mu.Unlock()
	}
	answer(w, http.StatusOK, map[string]any{"offset": now, "fertig": fertig})
}

// claim holds key for body; a piece still hanging there (a phone that came
// back after a cut) is stopped and waited for, as a backup's is.
func (im *importer) claim(key string, body *idleReader) bool {
	until := time.Now().Add(sicherungUebernahme)
	for {
		im.mu.Lock()
		old := im.busy[key]
		if old == nil {
			im.busy[key] = body
			im.mu.Unlock()
			return true
		}
		im.mu.Unlock()
		old.stop()
		if time.Now().After(until) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (im *importer) release(key string, body *idleReader) {
	im.mu.Lock()
	if im.busy[key] == body {
		delete(im.busy, key)
	}
	im.mu.Unlock()
}

// append writes at most limit bytes of body to the file at have, synced; a
// body longer than the piece is refused whole, one cut off keeps what came.
func (im *importer) append(person, id string, have, limit, rest int64, body io.Reader) (int64, int, string) {
	f, err := os.OpenFile(im.teil(person, id), os.O_WRONLY, 0)
	if err != nil {
		log.Printf("homes: standort-import: open %s: %v", id, err)
		return 0, http.StatusInternalServerError, saysImportStoreFail
	}
	defer f.Close()
	if _, err := f.Seek(have, io.SeekStart); err != nil {
		return 0, http.StatusInternalServerError, saysImportStoreFail
	}
	n, err := io.Copy(diskWriter{f}, io.LimitReader(body, limit))
	if err == nil {
		var one [1]byte
		if m, _ := io.ReadFull(body, one[:]); m > 0 {
			f.Truncate(have)
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
		log.Printf("homes: standort-import: write %s: %v", id, err)
		return n, storeStatus(err), storeSays(err)
	default:
		return n, http.StatusRequestTimeout, saysCut
	}
}

// GET /napi/standort/import/{id} → the state ([importStand]).
func (im *importer) stand(w http.ResponseWriter, r *http.Request, person string) {
	id := r.PathValue("id")
	st, err := im.read(person, id)
	if err != nil {
		fehler(w, http.StatusNotFound, saysImportUnknown)
		return
	}
	if st.Stand == standLaeuft {
		im.mu.Lock()
		if !im.laufend[person+"/"+id] { // Homes restarted mid-import
			im.start(person, id)
		}
		im.mu.Unlock()
	}
	answer(w, http.StatusOK, st)
}

// start runs person's import id; im.mu is held.
func (im *importer) start(person, id string) {
	key := person + "/" + id
	im.laufend[key] = true
	go func() {
		defer func() {
			im.mu.Lock()
			delete(im.laufend, key)
			im.mu.Unlock()
		}()
		im.lauf <- struct{}{}
		defer func() { <-im.lauf }()
		im.run(context.Background(), person, id)
	}()
}

// zaehler counts the bytes read, for the progress.
type zaehler struct {
	r io.Reader
	n int64
}

func (z *zaehler) Read(p []byte) (int, error) {
	n, err := z.r.Read(p)
	z.n += int64(n)
	return n, err
}

// run reads the file and hands its points to Reitti in batches; at the end
// the file is gone and the state says what came over, or why not.
func (im *importer) run(ctx context.Context, person, id string) {
	st, err := im.read(person, id)
	if err != nil || st.Stand != standLaeuft {
		return
	}
	part := im.teil(person, id)
	defer os.Remove(part)
	end := func(stand, text, why, grund string) {
		st.Stand, st.Text, st.Fehler, st.Grund = stand, text, why, grund
		if err := im.write(person, id, st); err != nil {
			log.Printf("homes: standort-import: %s/%s: %v", person, id, err)
		}
		log.Printf("homes: standort-import: %s/%s %s: %s, %d points, %d visits", person, id, stand, st.Format, st.Punkte, st.Besuche)
	}
	k, ok := im.r.Konto(person)
	if !ok {
		end(standFehler, "", saysImportNoToken, grundKeinSchluessel)
		return
	}
	f, err := os.Open(part)
	if err != nil {
		end(standFehler, "", saysImportUnknown, "")
		return
	}
	defer f.Close()
	st.Punkte, st.Besuche, st.Von, st.Bis = 0, 0, nil, nil
	z := &zaehler{r: f}
	batch := make([]reitti.Punkt, 0, importStapel)
	besuche, gesendet := 0, 0
	last := im.now()
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		var err error
		for try := 1; try <= importVersuche; try++ {
			_, err = im.r.Uebernehmen(ctx, k.Token, st.Name, batch)
			var u *reitti.Unavailable
			if err == nil || !errors.As(err, &u) || try == importVersuche {
				break
			}
			log.Printf("homes: standort-import: %s/%s: reitti did not take a batch (%v); again in %s", person, id, err, importPause)
			time.Sleep(importPause)
		}
		if err != nil {
			return err
		}
		gesendet += len(batch)
		st.Punkte, st.Besuche = gesendet, besuche/2
		batch = batch[:0]
		return nil
	}
	var von, bis time.Time
	format, err := reitti.Lesen(z, func(fd reitti.Fund) error {
		batch = append(batch, fd.Punkt)
		if fd.Besuch {
			besuche++
		}
		if von.IsZero() || fd.Zeit.Before(von) {
			von = fd.Zeit
		}
		if fd.Zeit.After(bis) {
			bis = fd.Zeit
		}
		if len(batch) >= importStapel {
			if err := flush(); err != nil {
				return err
			}
			st.Von, st.Bis = &von, &bis
		}
		if now := im.now(); now.Sub(last) >= importStandAlle {
			last = now
			st.Gelesen = z.n
			im.write(person, id, st)
		}
		return nil
	})
	st.Format = string(format)
	var fe *reitti.FormatError
	if err == nil || errors.As(err, &fe) { // what came before a cut goes over too
		if ferr := flush(); ferr != nil {
			err = ferr
		}
	}
	st.Gelesen = z.n
	if gesendet > 0 {
		st.Von, st.Bis = &von, &bis
	}
	var rf *reitti.ImportFehler
	switch {
	case err == nil && gesendet == 0:
		end(standFehler, "", saysImportNoPoints, "keine_orte")
	case err == nil:
		end(standFertig, uebernommen(st), "", "")
	case errors.Is(err, reitti.ErrFormat):
		end(standFehler, "", saysImportFormat, "format")
	case errors.As(err, &fe):
		log.Printf("homes: standort-import: %s/%s: %v", person, id, err)
		end(standFehler, "", "Die Datei ist nicht vollständig lesbar"+schonDa(st)+". Wurde sie beim Kopieren abgeschnitten? Teile sie dann noch einmal.", "format")
	case errors.Is(err, reitti.ErrToken):
		end(standFehler, "", saysReittiRefused+schonSatz(st), grundAbgelehnt)
	case errors.As(err, &rf):
		log.Printf("homes: standort-import: %s/%s: %v", person, id, err)
		end(standFehler, "", "Reitti hat die Orte nicht angenommen ("+rf.Grund+")"+schonDa(st)+".", grundReittiWeg)
	default:
		log.Printf("homes: standort-import: %s/%s: %v", person, id, err)
		end(standFehler, "", "Reitti nimmt gerade nichts an"+schonDa(st)+". Teile die Datei später noch einmal; was schon da ist, kommt nicht doppelt.", grundReittiWeg)
	}
}

// schonDa is "; Reitti hat bis dahin 1.234 Orte übernommen" when some came over.
func schonDa(st importStand) string {
	if st.Punkte == 0 {
		return ""
	}
	return "; Reitti hat bis dahin " + orte(st.Punkte) + " übernommen"
}

func schonSatz(st importStand) string {
	if st.Punkte == 0 {
		return ""
	}
	return " Bis dahin hat Reitti " + orte(st.Punkte) + " übernommen."
}

// orte is „1 Ort“, „12.345 Orte“.
func orte(n int) string {
	if n == 1 {
		return "1 Ort"
	}
	return zahl(n) + " Orte"
}

var monate = [...]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}

func monat(t time.Time) string { return monate[t.Month()-1] + " " + strconv.Itoa(t.Year()) }

// uebernommen is the end of an import in one sentence: „12.345 Orte von
// März 2014 bis Dezember 2024 übernommen.“
func uebernommen(st importStand) string {
	s := orte(st.Punkte)
	if st.Von != nil && st.Bis != nil {
		if a, b := monat(*st.Von), monat(*st.Bis); a == b {
			s += " aus dem " + a
		} else {
			s += " von " + a + " bis " + b
		}
	}
	s += " übernommen"
	switch {
	case st.Besuche == 1:
		s += ", darunter 1 Aufenthalt"
	case st.Besuche > 1:
		s += ", darunter " + zahl(st.Besuche) + " Aufenthalte"
	}
	return s + ". Reitti ordnet sie in den nächsten Minuten in deine Zeitleiste ein."
}

// zahl is n the German way: 12.345.
func zahl(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + zahl(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}
