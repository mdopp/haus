package pages

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/laib"
)

// saysNurImHaus is the owner's sentence (2026-10-28) for a backup with one place only.
const saysNurImHaus = "Gesichert, aber nur hier im Haus. Die Kopie außer Haus fehlt noch."

// adminOnlyService are the services the family does not use itself: one of
// them red is the admins' (Box), not a line on the family's Start (haus#173).
var adminOnlyService = map[string]bool{"llama": true, "werkstatt": true, "usage-metrics": true, "whoami": true, "esphome": true, "beets": true}

// familyLabel is a service in the family's lines: Laib's name for it and
// what it is, „Immich (Fotos)“ (owner 2026-10-27: the same names as Laib's page).
func familyLabel(s laib.Service) string {
	if d := strings.TrimSpace(s.Beschreibung); d != "" && d != s.Titel() {
		return s.Titel() + " (" + d + ")"
	}
	return s.Titel()
}

// guarantees renders the three lines the family sees first (haus#2):
// everything runs (or what does not, and that someone is on it), the last
// backup and where it is, the last change in plain words — Rückgängig only
// for admins, and on Laib's page (Haus decides nothing).
func guarantees(ready laib.Readiness, rows []laib.Service, admin bool, laibURL string) string {
	var b strings.Builder
	b.WriteString("<section class=guarantees>")
	// 1. Alles läuft
	var down []string
	for _, s := range rows {
		if s.State == "rot" {
			down = append(down, familyLabel(s))
		}
	}
	if len(down) == 0 {
		b.WriteString("<p class=gruen><b>Alles läuft.</b></p>")
	} else {
		who := "Der Operator schaut nach."
		if ready.Operator != "läuft" && ready.Operator != "startet" {
			who = "Der Admin ist informiert."
		}
		fmt.Fprintf(&b, "<p class=rot><b>%s %s gerade nicht erreichbar.</b> %s</p>", html.EscapeString(strings.Join(down, ", ")), map[bool]string{true: "sind", false: "ist"}[len(down) > 1], who)
	}
	// 2. Letzte Sicherung
	switch {
	case !ready.LastBackup.OK:
		b.WriteString("<p class=rot>Letzte Sicherung: <b>noch keine</b>.</p>")
	default:
		at, _ := time.Parse(time.RFC3339, ready.LastBackup.At)
		age := time.Since(at)
		cls := "gruen"
		if age > 26*time.Hour || ready.LastBackup.Repos < 2 {
			cls = "gelb"
		}
		if age > 72*time.Hour || ready.LastBackup.Repos < 1 {
			cls = "rot"
		}
		places := fmt.Sprintf("an %d Orten", ready.LastBackup.Repos)
		if ready.LastBackup.Repos == 1 {
			places = "an einem Ort"
		}
		fmt.Fprintf(&b, "<p class=%s>Letzte Sicherung: <b>%s</b>, %s.</p>", cls, when(at), places)
	}
	// 3. Letzte Änderung
	if ready.LastChange.Summary != "" {
		at, _ := time.Parse(time.RFC3339, ready.LastChange.At)
		fmt.Fprintf(&b, "<p>Letzte Änderung: <b>%s</b> (%s)", html.EscapeString(ready.LastChange.Summary), when(at))
		if admin && ready.LastChange.Reversible {
			fmt.Fprintf(&b, " — <a href=\"%s/inbox\">Rückgängig</a> <span class=k>(auf der Laib-Seite, mit der Folge davor)</span>", html.EscapeString(strings.TrimRight(laibURL, "/")))
		}
		b.WriteString("</p>")
	}
	b.WriteString("</section>")
	return b.String()
}

// when says a time the family way: heute 03:12, gestern 18:40, 09.10. 07:00.
func when(t time.Time) string {
	if t.IsZero() {
		return "unbekannt"
	}
	t = t.Local()
	now := time.Now()
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(today):
		return "heute " + t.Format("15:04")
	case !t.Before(today.Add(-24 * time.Hour)):
		return "gestern " + t.Format("15:04")
	}
	return t.Format("02.01. 15:04")
}

// familyBox is the box in the family's words on Start (haus#35, owner
// decision 2026-10-15): one calm line at the bottom when all is well —
// "Alles in Ordnung · eure Daten sind von gestern Abend gesichert". When
// a service is down or the data is not backed up, it is on top instead,
// saying what it means for the family and that someone is on it — amber
// (selbst) when the box mends it itself, red (hilfe) only when a person is
// needed: nobody automatic is on it, or the data is not backed up; the
// admin's words (services, places, the Operator) stay on /box. Laib not
// answering is #34's plain line, at the bottom. "Alles in Ordnung" only
// when quiet: no part of the page said something does not answer.
func familyBox(ready laib.Readiness, rows []laib.Service, err error, quiet bool, now time.Time, loc *time.Location) (top, bottom string) {
	if err != nil {
		return "", "<p class=ruhig><span class=\"punkt grau\"></span>Wie es der Box geht, lässt sich gerade nicht sagen.</p>"
	}
	var wrong []string
	var down []string
	for _, s := range rows {
		if s.State == "rot" && !adminOnlyService[s.Name] { // haus#173: what the family uses
			down = append(down, familyLabel(s))
		}
	}
	if len(down) > 0 {
		wrong = append(wrong, "Gerade nicht erreichbar: "+andList(down)+".")
	}
	if len(ready.Problems) > 0 && len(down) == 0 { // red beyond one service: disk, firewall … (laib#856)
		wrong = append(wrong, "An der Box stimmt gerade etwas nicht.")
	}
	at, _ := time.Parse(time.RFC3339, ready.LastBackup.At)
	saved, unsaved := "", false
	switch {
	case !ready.LastBackup.OK || ready.LastBackup.Repos < 1 || at.IsZero():
		wrong, unsaved = append(wrong, "Eure Daten sind noch nicht gesichert."), true
	case now.Sub(at) > 72*time.Hour:
		wrong, unsaved = append(wrong, "Eure Daten sind seit "+strconv.Itoa(int(now.Sub(at)/(24*time.Hour)))+" Tagen nicht gesichert."), true
	case ready.LastBackup.OnlyOnDataDisk || ready.LastBackup.Repos < 2:
		// haus#173, the owner 2026-10-28: one place is not „gesichert“ — the copy off the box is missing
		saved = saysNurImHaus
	default:
		saved = "eure Daten sind " + backupWords(at, now, loc) + " gesichert"
	}
	nurHier := saved == saysNurImHaus
	switch {
	case len(wrong) == 0 && nurHier:
		return "", "<p class=ruhig><span class=\"punkt gelb\"></span>" + saysNurImHaus + "</p>"
	case len(wrong) == 0 && quiet:
		return "", "<p class=ruhig><span class=punkt></span>Alles in Ordnung · " + saved + "</p>"
	case len(wrong) == 0:
		return "", "<p class=ruhig><span class=punkt></span>E" + saved[1:] + "</p>"
	}
	who, kind := "Wer die Box betreut, weiß Bescheid.", "hilfe"
	switch {
	case ready.OperatorWork.Incident != "": // the Operator is on it now (laib#856)
		who = "Die Box kümmert sich gerade darum."
		if !unsaved {
			kind = "selbst"
		}
	case ready.Operator == "läuft" || ready.Operator == "startet":
		who = "Die Box versucht, das selbst zu beheben."
		if !unsaved {
			kind = "selbst"
		}
	}
	var b strings.Builder
	b.WriteString("<section class=\"achtung " + kind + "\" role=status aria-label=\"Die Box\"><p>" + html.EscapeString(strings.Join(wrong, " ")) + "</p><p>" + who + "</p>")
	switch {
	case nurHier:
		b.WriteString("<p>" + saysNurImHaus + "</p>")
	case saved != "":
		b.WriteString("<p>E" + saved[1:] + ".</p>")
	}
	b.WriteString("</section>")
	return b.String(), ""
}

// backupWords says when the last backup was, the way the family says it:
// von heute Nacht, von gestern Abend, von Samstagabend, vom 1. Oktober.
func backupWords(at, now time.Time, loc *time.Location) string {
	at = at.In(loc)
	part := "Abend"
	switch h := at.Hour(); {
	case h < 5, h >= 23:
		part = "Nacht"
	case h < 10:
		part = "Morgen"
	case h < 12:
		part = "Vormittag"
	case h < 14:
		part = "Mittag"
	case h < 18:
		part = "Nachmittag"
	}
	switch d := daysAgo(at, now, loc); {
	case d <= 0:
		return "von heute " + part
	case d == 1:
		return "von gestern " + part
	case d == 2:
		return "von vorgestern " + part
	case d <= 6:
		return "von " + weekdays[at.Weekday()] + strings.ToLower(part)
	}
	return "vom " + strconv.Itoa(at.Day()) + ". " + months[at.Month()-1]
}
