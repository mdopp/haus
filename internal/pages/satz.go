package pages

import (
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Errors in the family's words (haus#176, as laib#855): an error that
// already reads as a German sentence for a person — laib's and the
// backends' own refusals („Den Benutzer anna gibt es schon.“) — is said as
// it is; anything technical (an address, a protocol, a Go error) goes to
// the log, and the person reads the fallback sentence instead.

var technisch = regexp.MustCompile(`(?i)https?://|\bdial\b|\btcp\b|\beof\b|context deadline|\bjson\b|\bnil\b|connection|status \d|\.go:\d|\b[a-z]+_[a-z_]+\b|\b(?:laib|haus|mcp|caldav|radicale|paperless)\s*:`)

// familySentence is err as a person may read it, else fallback (and err in the log, under where).
func familySentence(where string, err error, fallback string) string {
	if err == nil {
		return ""
	}
	t := strings.TrimSpace(err.Error())
	if t != "" && !technisch.MatchString(t) && strings.ToUpper(t[:1]) == t[:1] && strings.ContainsAny(t[len(t)-1:], ".!?“") {
		return t
	}
	log.Printf("haus: %s: %v", where, err)
	return fallback
}

// dauerWorte is a duration as a person says it: „30 Sekunden“, „eine Minute“, „2 Minuten“.
func dauerWorte(d time.Duration) string {
	switch s := int(d.Round(time.Second) / time.Second); {
	case s < 60:
		return strconv.Itoa(s) + " Sekunden"
	case s < 120:
		return "einer Minute"
	default:
		return strconv.Itoa((s+30)/60) + " Minuten"
	}
}

// saysLaibSilent is Laib not answering, on the admins' Box pages; the raw error folded beneath.
const saysLaibSilent = "Laib antwortet gerade nicht. Die Seite gleich neu laden; bleibt es so, steht unter Einzelheiten, was fehlt."
