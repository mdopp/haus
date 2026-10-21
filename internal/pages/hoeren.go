package pages

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/mdopp/haus/internal/ha"
)

// STT is the speech-to-text the Sprechen field's microphone uses (haus#63):
// Home Assistant's, through Haus, so a sentence spoken into the field is
// heard by Whisper on the box, never by a browser's cloud. Provider is
// HA's provider (HAUS_STT_PROVIDER, e.g. stt.faster_whisper), Language as
// that provider names it (HAUS_STT_LANGUAGE, default de). No provider, or
// no HA: no microphone.
type STT struct {
	Provider, Language string
}

// HoerenPath is where the field's microphone sends what it recorded.
const HoerenPath = "/sprechen/hoeren"

// sttWait is how long Haus waits for HA's speech-to-text; maxSpeech caps a
// recording (16 kHz, 16 bit, mono: about a minute; the page stops at 30 s).
var sttWait = 60 * time.Second

const maxSpeech = 2 << 20

// What the microphone says when it gets no words (haus#63): plain, and
// what to do instead.
const (
	saysNotHeard   = "Nichts verstanden. Sprich etwas näher am Telefon und versuch es noch einmal."
	saysNoSTT      = "Die Spracherkennung antwortet gerade nicht. Tippe deine Frage lieber ein."
	saysBadSpeech  = "Die Aufnahme ließ sich nicht lesen. Versuch es noch einmal."
	saysSpeechLong = "Die Aufnahme ist zu lang: höchstens eine halbe Minute."
)

// canHear says whether the field gets a microphone.
func canHear(cfg StartConfig) bool {
	return cfg.HA != nil && cfg.STT.Provider != "" && ha.STTProviderRE.MatchString(cfg.STT.Provider)
}

// micButton is the microphone beside the field: hidden until the page's
// script knows the browser can record.
const micButton = `<button type=button class=mikro aria-label="Sprechen statt tippen" aria-pressed=false hidden>` +
	`<svg viewBox="0 0 24 24" aria-hidden=true><rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5.5 11a6.5 6.5 0 0 0 13 0M12 17.5V21"/></svg></button>`

// Hoeren turns the microphone's recording into words (haus#63): a 16-bit
// mono PCM WAV from the page, passed to HA's speech-to-text as it is, and
// the words back as {"ok":true,"text":…}; the page then sends them like
// typed ones. Only a person, only from Haus's own page; nothing is kept —
// not the recording, not the words. Anything that is no words is one
// plain line ({"ok":false,"says":…}), HA's detail in the log.
func Hoeren(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Hier geht nur eine Aufnahme aus dem Sprechen-Feld hin.", http.StatusMethodNotAllowed)
			return
		}
		if !canHear(cfg) {
			http.NotFound(w, r)
			return
		}
		if !fromHaus(r) {
			http.Error(w, "Sprechen geht nur von Haus' eigener Seite.", http.StatusForbidden)
			return
		}
		speech, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSpeech))
		var tooBig *http.MaxBytesError
		switch {
		case errors.As(err, &tooBig):
			heard(w, http.StatusRequestEntityTooLarge, "", saysSpeechLong)
			return
		case err != nil:
			heard(w, http.StatusBadRequest, "", saysBadSpeech)
			return
		}
		rate, ok := wavRate(speech)
		if !ok {
			heard(w, http.StatusBadRequest, "", saysBadSpeech)
			return
		}
		lang := cfg.STT.Language
		if lang == "" {
			lang = "de"
		}
		ctx, cancel := context.WithTimeout(r.Context(), sttWait)
		defer cancel()
		text, err := cfg.HA.Transcribe(ctx, cfg.STT.Provider, lang, rate, speech)
		switch {
		case errors.Is(err, ha.ErrNotUnderstood):
			heard(w, http.StatusOK, "", saysNotHeard)
		case err != nil:
			log.Printf("haus: sprechen: speech-to-text %s: %v", cfg.STT.Provider, err)
			heard(w, http.StatusBadGateway, "", saysNoSTT)
		default:
			cfg.Usage.Count("sprechen.mikrofon") // that the microphone was used, never what was said (haus#50)
			heard(w, http.StatusOK, text, "")
		}
	}
}

// heard is Hoeren's answer: the words, or the line why there are none.
func heard(w http.ResponseWriter, code int, text, says string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if says != "" {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "says": says})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "text": text})
}

// wavRate reads a WAV's header: 16-bit mono PCM with some sound, at a rate
// speech-to-text takes (8–48 kHz). Anything else is not a recording of the
// page's.
func wavRate(b []byte) (int, bool) {
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return 0, false
	}
	rate, pcm := 0, false
	for off := 12; off+8 <= len(b); {
		id, size := string(b[off:off+4]), int(binary.LittleEndian.Uint32(b[off+4:off+8]))
		body := off + 8
		switch id {
		case "fmt ":
			if size < 16 || body+size > len(b) {
				return 0, false
			}
			format, channels := binary.LittleEndian.Uint16(b[body:]), binary.LittleEndian.Uint16(b[body+2:])
			rate = int(binary.LittleEndian.Uint32(b[body+4:]))
			bits := binary.LittleEndian.Uint16(b[body+14:])
			pcm = format == 1 && channels == 1 && bits == 16 && rate >= 8000 && rate <= 48000
		case "data":
			return rate, pcm && size > 0 && body+size <= len(b)
		}
		if size < 0 || size > len(b) {
			return 0, false
		}
		off = body + size + size%2
	}
	return 0, false
}
