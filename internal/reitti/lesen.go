package reitti

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// A location history the person exported elsewhere and shares to the
// Homes app (napi/standort_import.go): Homes reads it here, point by point
// and never whole (a Google export is hundreds of MB), and hands the points
// to Reitti's token import (Uebernehmen). Reitti reads Google's formats only
// on its logged-in settings page, which Homes cannot reach with a token, so
// Homes reads them the way Reitti's own importers do
// (service/importer/Google*Importer.java at v5.9.0): the same points, the
// same times, the same default accuracies.

// Format is which kind of history a file was.
type Format string

const (
	FormatRecords     Format = "records"      // Google Takeout, Standortverlauf/Records.json: {"locations": […]}
	FormatTimeline    Format = "timeline"     // Google Maps on an Android phone, Timeline.json: {"semanticSegments": […]}
	FormatTimelineIOS Format = "timeline-ios" // Google Maps on an iPhone: […] of segments
	FormatGeoJSON     Format = "geojson"      // a FeatureCollection of Points with a time each
	FormatGPX         Format = "gpx"          // <gpx> with <trkpt>s
)

// ErrFormat is a file that is none of the formats Homes reads.
var ErrFormat = errors.New("reitti: not a location history Homes reads")

// Fund is one point of a history file. Besuch marks the start or end of a
// stay at a place (a Timeline visit is two points, as Reitti makes it).
type Fund struct {
	Punkt
	Besuch bool
}

// FormatError is a file that broke off or went wrong inside: what was read
// before it has been handed on.
type FormatError struct {
	Format Format
	Err    error
}

func (e *FormatError) Error() string { return fmt.Sprintf("reitti: %s: %v", e.Format, e.Err) }
func (e *FormatError) Unwrap() error { return e.Err }

// Default accuracies, Reitti's own for each format.
const (
	genauGoogle  = 10.0 // GoogleRecordsImporter, BaseGoogleTimelineImporter
	genauGeoJSON = 50.0 // GeoJsonImporter
	genauGPX     = 10.0 // GpxImporter; 120 when hdop > 5
)

// Lesen reads a history from r and hands each point to emit, in file
// order. It says which format the file was; an error from emit stops it
// and comes back as it is. A file Homes does not know is ErrFormat; one
// that is cut off or broken inside is *FormatError.
func Lesen(r io.Reader, emit func(Fund) error) (Format, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	first, err := firstByte(br)
	if err != nil {
		return "", ErrFormat
	}
	switch first {
	case '<':
		return FormatGPX, wrap(FormatGPX, readGPX(br, emit))
	case '[':
		dec := json.NewDecoder(br)
		return FormatTimelineIOS, wrap(FormatTimelineIOS, readSegments(dec, emit))
	case '{':
		return readObject(json.NewDecoder(br), emit)
	}
	return "", ErrFormat
}

func wrap(f Format, err error) error {
	var fe *FormatError
	switch {
	case err == nil, errors.Is(err, ErrFormat), errors.As(err, &fe):
		return err
	case errors.Is(err, errEmit):
		return errors.Unwrap(err)
	}
	return &FormatError{Format: f, Err: err}
}

// errEmit wraps emit's own error, so it is not taken for the file's.
var errEmit = errors.New("emit")

type emitErr struct{ err error }

func (e emitErr) Error() string        { return e.err.Error() }
func (e emitErr) Is(target error) bool { return target == errEmit }
func (e emitErr) Unwrap() error        { return e.err }

func send(emit func(Fund) error, f Fund) error {
	if err := emit(f); err != nil {
		return emitErr{err}
	}
	return nil
}

// firstByte is the first byte that is not a BOM or white space, unread.
func firstByte(br *bufio.Reader) (byte, error) {
	if b, err := br.Peek(3); err == nil && bytes.Equal(b, []byte{0xEF, 0xBB, 0xBF}) {
		br.Discard(3)
	}
	for {
		b, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		if b != ' ' && b != '\t' && b != '\r' && b != '\n' {
			br.UnreadByte()
			return b, nil
		}
	}
}

// readObject is a top-level object: the first key Homes knows names the
// format; every other value is passed over without being kept.
func readObject(dec *json.Decoder, emit func(Fund) error) (Format, error) {
	if _, err := dec.Token(); err != nil { // {
		return "", ErrFormat
	}
	var found Format
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return found, wrap(orUnknown(found), err)
		}
		key, _ := t.(string)
		var f Format
		var read func(*json.Decoder, func(Fund) error) error
		switch key {
		case "locations":
			f, read = FormatRecords, readRecords
		case "semanticSegments":
			f, read = FormatTimeline, readSegments
		case "features":
			f, read = FormatGeoJSON, readFeatures
		}
		if read == nil || (found != "" && f != found) {
			if err := skip(dec); err != nil {
				return found, wrap(orUnknown(found), err)
			}
			continue
		}
		found = f
		if err := read(dec, emit); err != nil {
			return found, wrap(found, err)
		}
	}
	if found == "" {
		return "", ErrFormat
	}
	return found, nil
}

func orUnknown(f Format) Format {
	if f == "" {
		return "json"
	}
	return f
}

// skip passes over one value, however large, keeping none of it.
func skip(dec *json.Decoder) error {
	depth := 0
	for {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		switch t {
		case json.Delim('{'), json.Delim('['):
			depth++
		case json.Delim('}'), json.Delim(']'):
			depth--
		}
		if depth == 0 {
			return nil
		}
	}
}

// eachIn decodes each element of the array that comes next into a new T;
// an element of the wrong shape is passed over, as Reitti does.
func eachIn[T any](dec *json.Decoder, each func(T) error) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	if t != json.Delim('[') {
		return errors.New("not an array")
	}
	for dec.More() {
		var v T
		if err := dec.Decode(&v); err != nil {
			var te *json.UnmarshalTypeError
			if errors.As(err, &te) {
				continue // the decoder read the whole element; the next one is next
			}
			return err
		}
		if err := each(v); err != nil {
			return err
		}
	}
	_, err = dec.Token() // ]
	return err
}

// record is one entry of Records.json (GoogleRecordsImporter.java); older
// exports have timestampMs instead of timestamp.
type record struct {
	LatE7       *float64 `json:"latitudeE7"`
	LngE7       *float64 `json:"longitudeE7"`
	Timestamp   string   `json:"timestamp"`
	TimestampMs string   `json:"timestampMs"`
	Accuracy    *float64 `json:"accuracy"`
	Altitude    *float64 `json:"altitude"`
}

func readRecords(dec *json.Decoder, emit func(Fund) error) error {
	return eachIn(dec, func(r record) error {
		if r.LatE7 == nil || r.LngE7 == nil {
			return nil
		}
		t, ok := googleTime(r.Timestamp, r.TimestampMs)
		if !ok {
			return nil
		}
		lat, lng := e7(*r.LatE7), e7(*r.LngE7)
		if !onEarth(lat, lng) {
			return nil
		}
		p := Punkt{Zeit: t, Breite: lat, Laenge: lng, Genauigkeit: genauGoogle}
		if r.Accuracy != nil && finite(*r.Accuracy) && *r.Accuracy >= 0 {
			p.Genauigkeit = *r.Accuracy
		}
		if r.Altitude != nil && finite(*r.Altitude) {
			p.Hoehe = r.Altitude
		}
		return send(emit, Fund{Punkt: p})
	})
}

// e7 is a coordinate Google stores times 10⁷; an old export's overflowed
// int32 comes back.
func e7(v float64) float64 {
	if v > 1800000000 {
		v -= 4294967296
	}
	return v / 1e7
}

func googleTime(iso, ms string) (time.Time, bool) {
	if iso != "" {
		t, err := time.Parse(time.RFC3339Nano, iso)
		return t.UTC(), err == nil
	}
	if n, err := strconv.ParseInt(ms, 10, 64); err == nil && ms != "" {
		return time.UnixMilli(n).UTC(), true
	}
	return time.Time{}, false
}

// segment is one semantic segment, of the Android export (an object with
// semanticSegments) and the iPhone's (an array of them): a visit is its
// place at the start and at the end; a path is its points
// (GoogleAndroidTimelineImporter.java, GoogleIOSTimelineImporter.java).
type segment struct {
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
	Visit     *struct {
		TopCandidate struct {
			PlaceLocation json.RawMessage `json:"placeLocation"` // Android {"latLng": "…°, …°"}, iPhone "geo:…,…"
		} `json:"topCandidate"`
	} `json:"visit"`
	TimelinePath []struct {
		Point  string `json:"point"`
		Time   string `json:"time"`                               // Android
		Offset string `json:"durationMinutesOffsetFromStartTime"` // iPhone
	} `json:"timelinePath"`
}

func readSegments(dec *json.Decoder, emit func(Fund) error) error {
	return eachIn(dec, func(s segment) error {
		start, err1 := time.Parse(time.RFC3339Nano, s.StartTime)
		end, err2 := time.Parse(time.RFC3339Nano, s.EndTime)
		if err1 != nil || err2 != nil {
			return nil
		}
		start, end = start.UTC().Truncate(time.Second), end.UTC().Truncate(time.Second)
		if s.Visit != nil {
			if lat, lng, ok := placeOf(s.Visit.TopCandidate.PlaceLocation); ok {
				for _, t := range []time.Time{start, end} {
					if err := send(emit, Fund{Punkt: Punkt{Zeit: t, Breite: lat, Laenge: lng, Genauigkeit: genauGoogle}, Besuch: true}); err != nil {
						return err
					}
				}
			}
		}
		for _, pp := range s.TimelinePath {
			lat, lng, ok := LatLng(pp.Point)
			if !ok {
				continue
			}
			var t time.Time
			if pp.Time != "" {
				tt, err := time.Parse(time.RFC3339Nano, pp.Time)
				if err != nil {
					continue
				}
				t = tt.UTC().Truncate(time.Second)
			} else if m, err := strconv.ParseInt(strings.TrimSpace(pp.Offset), 10, 64); err == nil {
				t = start.Add(time.Duration(m) * time.Minute)
			} else {
				continue
			}
			if err := send(emit, Fund{Punkt: Punkt{Zeit: t, Breite: lat, Laenge: lng, Genauigkeit: genauGoogle}}); err != nil {
				return err
			}
		}
		return nil
	})
}

func placeOf(raw json.RawMessage) (float64, float64, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return LatLng(s)
	}
	var o struct {
		LatLng string `json:"latLng"`
	}
	if json.Unmarshal(raw, &o) == nil {
		return LatLng(o.LatLng)
	}
	return 0, 0, false
}

// LatLng reads Google's "53.8633043°, 10.7011529°" and "geo:55.605843,13.007508".
func LatLng(s string) (float64, float64, bool) {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, "geo:"); ok {
		s = rest
	} else {
		s = strings.ReplaceAll(s, "°", "")
	}
	a, b, ok := strings.Cut(s, ",")
	if !ok {
		return 0, 0, false
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(a), 64)
	lng, err2 := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if err1 != nil || err2 != nil || !onEarth(lat, lng) {
		return 0, 0, false
	}
	return lat, lng, true
}

// geoFeature is one GeoJSON feature; only a Point with a time is a place
// (GeoJsonImporter.java: the same property names, in the same order).
type geoFeature struct {
	Geometry *struct {
		Type        string    `json:"type"`
		Coordinates []float64 `json:"coordinates"`
	} `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

var (
	geoZeit  = []string{"timestamp", "time", "datetime", "date", "when"}
	geoGenau = []string{"accuracy", "acc", "precision", "hdop"}
	geoHoehe = []string{"elevation", "ele", "altitude", "alt", "height"}
)

func readFeatures(dec *json.Decoder, emit func(Fund) error) error {
	return eachIn(dec, func(f geoFeature) error {
		g := f.Geometry
		if g == nil || g.Type != "Point" || len(g.Coordinates) < 2 {
			return nil
		}
		lng, lat := g.Coordinates[0], g.Coordinates[1]
		if !onEarth(lat, lng) {
			return nil
		}
		t, ok := geoTime(first(f.Properties, geoZeit))
		if !ok {
			return nil
		}
		p := Punkt{Zeit: t, Breite: lat, Laenge: lng, Genauigkeit: genauGeoJSON}
		if a, ok := number(first(f.Properties, geoGenau)); ok && a >= 0 {
			p.Genauigkeit = a
		}
		if len(g.Coordinates) > 2 && finite(g.Coordinates[2]) {
			h := g.Coordinates[2]
			p.Hoehe = &h
		} else if h, ok := number(first(f.Properties, geoHoehe)); ok {
			p.Hoehe = &h
		}
		return send(emit, Fund{Punkt: p})
	})
}

func first(m map[string]any, names []string) any {
	for _, n := range names {
		if v, ok := m[n]; ok {
			return v
		}
	}
	return nil
}

// geoTime is epoch seconds or an ISO time, as GeoJsonImporter reads it.
func geoTime(v any) (time.Time, bool) {
	switch x := v.(type) {
	case float64:
		if finite(x) {
			return time.Unix(int64(x), 0).UTC(), true
		}
	case string:
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return time.Unix(n, 0).UTC(), true
		}
		if t, err := time.Parse(time.RFC3339Nano, x); err == nil {
			return t.UTC().Truncate(time.Second), true
		}
	}
	return time.Time{}, false
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, finite(x)
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil && finite(f)
	}
	return 0, false
}

// trkpt is one GPX track point (GpxImporter.java: <trkpt> only; accuracy
// from an <accuracy> anywhere in it, else from <hdop>).
type trkpt struct {
	Lat  string `xml:"lat,attr"`
	Lon  string `xml:"lon,attr"`
	Ele  string `xml:"ele"`
	Time string `xml:"time"`
	Hdop string `xml:"hdop"`
	Ext  struct {
		Inner []byte `xml:",innerxml"`
	} `xml:"extensions"`
}

func readGPX(r io.Reader, emit func(Fund) error) error {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	root := true
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			if root {
				return ErrFormat
			}
			return nil
		}
		if err != nil {
			if root {
				return ErrFormat
			}
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if root {
			if se.Name.Local != "gpx" {
				return ErrFormat
			}
			root = false
			continue
		}
		if se.Name.Local != "trkpt" {
			continue
		}
		var tp trkpt
		if err := dec.DecodeElement(&tp, &se); err != nil {
			return err
		}
		lat, err1 := strconv.ParseFloat(strings.TrimSpace(tp.Lat), 64)
		lng, err2 := strconv.ParseFloat(strings.TrimSpace(tp.Lon), 64)
		t, err3 := time.Parse(time.RFC3339Nano, strings.TrimSpace(tp.Time))
		if err1 != nil || err2 != nil || err3 != nil || !onEarth(lat, lng) {
			continue
		}
		p := Punkt{Zeit: t.UTC(), Breite: lat, Laenge: lng, Genauigkeit: genauGPX}
		if a, ok := gpxAccuracy(tp.Ext.Inner); ok {
			p.Genauigkeit = a
		} else if h, err := strconv.ParseFloat(strings.TrimSpace(tp.Hdop), 64); err == nil && h > 5 {
			p.Genauigkeit = 120
		}
		if h, err := strconv.ParseFloat(strings.TrimSpace(tp.Ele), 64); err == nil && finite(h) {
			p.Hoehe = &h
		}
		if err := send(emit, Fund{Punkt: p}); err != nil {
			return err
		}
	}
}

// gpxAccuracy is an <accuracy> element anywhere in a point's extensions.
func gpxAccuracy(inner []byte) (float64, bool) {
	if !bytes.Contains(inner, []byte("accuracy")) {
		return 0, false
	}
	dec := xml.NewDecoder(bytes.NewReader(inner))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err != nil {
			return 0, false
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "accuracy" {
			var s string
			if dec.DecodeElement(&s, &se) == nil {
				if a, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && finite(a) && a >= 0 {
					return a, true
				}
			}
			return 0, false
		}
	}
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func onEarth(lat, lng float64) bool {
	return finite(lat) && finite(lng) && lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}
