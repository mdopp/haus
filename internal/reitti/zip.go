package reitti

import (
	"archive/zip"
	"errors"
	"io"
	"path"
	"slices"
	"sort"
	"strings"
)

// A Google Takeout comes as a ZIP (owner 2026-11-04): Homes reads every file in it that it
// knows, wherever it sits — Records.json under whatever the folder is called in the person's
// language, the Semantic Location History's monthly files, Timeline.json, GPX, GeoJSON —
// known by what is in it, never by its folder's name. The ZIP is read from the staged file,
// one member at a time, never whole.

// FormatZIP is a ZIP of histories.
const FormatZIP Format = "zip"

// ErrZIPLeer is a ZIP with no history Homes reads in it.
var ErrZIPLeer = errors.New("reitti: no location history Homes reads in the zip")

const (
	zipMaxDateien  = 20000           // members looked at, a Takeout's own few hundred and room
	zipMaxEntpackt = int64(40) << 30 // what is unpacked in all, against a ZIP that claims less than it holds
)

// ErrTakeoutTeil is a part of a Google Takeout with no history in it (owner 2026-11-04: Takeout
// splits at 2 GB, each file whole in one part): the history is in another part.
var ErrTakeoutTeil = errors.New("reitti: no location history in this part of the takeout")

// errGenug stops a look at a file once its format is known.
var errGenug = errors.New("reitti: enough")

// Roh are the hours a person's raw points came for (Records.json, Timeline.json, GPX,
// GeoJSON), kept across imports — a Takeout in two parts is two imports: the Semantic
// Location History's points are taken only for hours none did. Raw points always go; Reitti
// keeps a point it has (same device, same second) once.
type Roh map[int64]bool

// filter lets fd through unless it is a Semantic point of an hour raw points came for; it
// counts what it let through.
func (roh Roh) filter(format Format, n *int, emit func(Fund) error) func(Fund) error {
	return func(fd Fund) error {
		h := fd.Zeit.Unix() / 3600
		if format == FormatSemantic && roh[h] {
			return nil
		}
		if format != FormatSemantic {
			roh[h] = true
		}
		*n++
		return emit(fd)
	}
}

// LesenDatei reads the history in f (size bytes), a file or a ZIP of them, and hands each
// point to emit, the Semantic Location History's only for hours roh has no raw point for (a
// ZIP's raw files are read first); roh gains the raw points' hours. gelesen is told how far
// into f it has come. It says the format (FormatZIP for a ZIP) and how many files gave points.
// A ZIP without a history Homes reads is ErrZIPLeer, ErrTakeoutTeil when it is a Takeout's;
// a broken member is passed over.
func LesenDatei(f io.ReaderAt, size int64, roh Roh, gelesen func(int64), emit func(Fund) error) (Format, int, error) {
	head, n := make([]byte, 4), 0
	if m, _ := f.ReadAt(head, 0); m < 4 || (string(head) != "PK\x03\x04" && string(head) != "PK\x05\x06") {
		format, _ := Lesen(io.NewSectionReader(f, 0, size), func(Fund) error { return errGenug })
		format, err := Lesen(&zaehler{r: io.NewSectionReader(f, 0, size), gelesen: gelesen}, roh.filter(format, &n, emit))
		return format, 1, err
	}
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return FormatZIP, 0, &FormatError{Format: FormatZIP, Err: err}
	}
	budget := zipMaxEntpackt
	ds := kandidaten(zr, &budget)
	sort.SliceStable(ds, func(i, j int) bool { return ds[i].format != FormatSemantic && ds[j].format == FormatSemantic })
	dateien, done := 0, int64(0)
	for _, d := range ds {
		var stop error
		punkte := 0
		_, err := lesenIn(d.zf, &budget, roh.filter(d.format, &punkte, func(fd Fund) error { stop = emit(fd); return stop }))
		var fe *FormatError
		switch {
		case stop != nil:
			return FormatZIP, dateien, stop
		case err != nil && !errors.Is(err, ErrFormat) && !errors.As(err, &fe):
			return FormatZIP, dateien, err
		case punkte > 0:
			dateien++
		}
		done += int64(d.zf.CompressedSize64)
		gelesen(min(done, size))
	}
	gelesen(size)
	if dateien == 0 && slices.ContainsFunc(zr.File, func(zf *zip.File) bool { return strings.HasPrefix(zf.Name, "Takeout/") }) {
		return FormatZIP, 0, ErrTakeoutTeil
	} else if dateien == 0 {
		return FormatZIP, 0, ErrZIPLeer
	}
	return FormatZIP, dateien, nil
}

// datei is a member of a ZIP Homes knows, and its format.
type datei struct {
	zf     *zip.File
	format Format
}

// kandidaten are a ZIP's members whose content is a history Homes reads.
func kandidaten(zr *zip.Reader, budget *int64) []datei {
	var ds []datei
	for i, zf := range zr.File {
		name := path.Base(zf.Name)
		if i == zipMaxDateien || *budget <= 0 {
			break
		}
		if zf.FileInfo().IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(zf.Name, "__MACOSX/") ||
			!strings.Contains(" .json .gpx .geojson ", " "+strings.ToLower(path.Ext(name))+" ") {
			continue
		}
		if format, err := lesenIn(zf, budget, func(Fund) error { return errGenug }); format != "" && !errors.Is(err, ErrFormat) {
			ds = append(ds, datei{zf, format})
		}
	}
	return ds
}

// lesenIn reads one member of a ZIP, what is unpacked counted against budget.
func lesenIn(zf *zip.File, budget *int64, emit func(Fund) error) (Format, error) {
	r, err := zf.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()
	lr := &io.LimitedReader{R: r, N: *budget}
	defer func() { *budget = lr.N }()
	format, err := Lesen(lr, emit)
	if errors.Is(err, errGenug) {
		err = nil
	}
	return format, err
}

// zaehler tells how far a reader has come.
type zaehler struct {
	r       io.Reader
	n       int64
	gelesen func(int64)
}

func (z *zaehler) Read(p []byte) (int, error) {
	n, err := z.r.Read(p)
	z.n += int64(n)
	z.gelesen(z.n)
	return n, err
}
