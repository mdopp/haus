package pages

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/texte"
)

// achtung is the Übersicht's top (owner 2026-11-06: „Was braucht mich?“):
// only what needs the admin, red before yellow — the services not green,
// laib diag's red and yellow findings, the due Box-Aufgaben, the decisions
// waiting — else one calm line. The guarantees and the newest morning
// report follow (haus#2; the journal is Laib's page's, haus#157).
func achtung(ctx context.Context, r *http.Request, cfg BoxConfig) string {
	var ready laib.Readiness
	var rows []laib.Service
	var found laib.Befunde
	var waiting []laib.Proposal
	var reports []laib.Entry
	var err, derr, ierr, rerr error
	fetchCtx(ctx, func(ctx context.Context) { ready, rows, err = cfg.Laib.Status(ctx) },
		func(ctx context.Context) { found, derr = cfg.Laib.Befunde(ctx) },
		func(ctx context.Context) { waiting, ierr = cfg.Laib.Inbox(ctx) },
		func(ctx context.Context) { reports, rerr = cfg.Laib.JournalLatest(ctx, 1, "report") })
	if err != nil {
		return "<h1>Box</h1><p class=rot>" + saysLaibSilent + "</p>" + einzelheiten(err.Error())
	}
	lines := map[string][]string{} // by class: rot, gelb, k
	add := func(class, what string) {
		lines[class] = append(lines[class], "<li>"+dot(class, cmp.Or(map[string]string{"k": "Hinweis"}[class], class))+"<span class=was>"+what+"</span></li>")
	}
	notGreen(rows, found, func(class string, s *laib.Service, f *laib.Finding) {
		if s != nil {
			add(class, serviceLabel(*s)+aboutIt(s.Detail))
		} else {
			add(class, "<b>"+html.EscapeString(cmp.Or(f.Say, f.Probe))+"</b>"+aboutIt(f.Detail))
		}
	})
	switch {
	case errors.Is(derr, laib.ErrNoTool): // a Laib without befunde: diag's red lines in laib status (laib#856)
		for _, p := range ready.Problems {
			var line string
			json.Unmarshal(p, &line)
			add("rot", aboutIt(line))
		}
		add("k", texte.T("box.diag-fehlt"))
	case derr != nil:
		add("gelb", texte.T("box.diag-stumm"))
	case found.At == nil: // never „Alles in Ordnung“ over what was not measured
		add("k", texte.T("box.diag-nie"))
	}
	if ready.Lock != "" {
		add("gelb", "gesperrt: "+html.EscapeString(ready.Lock))
	}
	if ready.Drift > 0 { // the running box differs from what Laib rendered
		add("gelb", fmt.Sprintf(texte.T("box.drift"), ready.Drift))
	}
	if ready.AufgabenFaellig > 0 { // the Box-Aufgaben's badge (haus#115)
		add("gelb", "<a href="+AufgabenPath+">"+texte.Anzahl(ready.AufgabenFaellig, texte.T("box.faellig"), texte.T("box.faellig.n"))+"</a>")
	}
	if ierr != nil {
		add("gelb", texte.T("box.inbox-stumm"))
	} else if n := len(waiting); n > 0 { // deciding is Laib's page's (ADR 0012, haus#157)
		add("k", laibLink(cfg, "/inbox", texte.Anzahl(n, texte.T("box.wartet"), texte.T("box.wartet.n"))))
	}
	foot := kept(ready, isAdmin(r), cfg)
	if found.At != nil {
		foot += " · " + fmt.Sprintf(texte.T("box.gemessen"), seit(*found.At, time.Now()))
	}
	top := "<p><b class=gruen>" + texte.T("box.ruhig") + "</b> · " + foot + "</p>"
	if all := append(append(lines["rot"], lines["gelb"]...), lines["k"]...); len(all) > 0 {
		top = "<ul class=zeilen>" + strings.Join(all, "") + "</ul><p class=k>" + foot + "</p>"
	}
	return "<h1>Box</h1>" + top + morningReport(reports, rerr) + "<p>" + laibLink(cfg, "/journal", "Das Journal auf der Laib-Seite") + "</p>"
}

// notGreen calls each for every service not green, then for laib diag's
// red and yellow findings beyond those services: the Übersicht's lines,
// and what Start's Braucht dich counts for an admin (one rule for both).
func notGreen(rows []laib.Service, found laib.Befunde, each func(class string, s *laib.Service, f *laib.Finding)) {
	shown := map[string]bool{}
	for i := range rows {
		if c := cls(rows[i].State); c != "gruen" {
			shown[rows[i].Name] = true
			each(c, &rows[i], nil)
		}
	}
	for i, f := range found.Findings {
		if (f.Severity == "rot" || f.Severity == "gelb") && !shown[f.Service] {
			each(f.Severity, nil, &found.Findings[i])
		}
	}
}

// seit says how long ago diag measured: vor 12 min, vor 3 h, else the day.
func seit(at, now time.Time) string {
	switch d := now.Sub(at); {
	case d < time.Minute:
		return "gerade eben"
	case d < 2*time.Hour:
		return fmt.Sprintf("vor %d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("vor %d h", int(d.Hours()))
	}
	return when(at)
}

// aboutIt is a line's detail below it, Box-Aufgabe #N a link.
func aboutIt(detail string) string {
	if d := oneLine(detail); d != "" {
		return "<span class=text>" + linkAufgaben(html.EscapeString(trim(d, 160))) + "</span>"
	}
	return ""
}

// kept is the guarantees in one line (haus#2): the last backup and where,
// the last change in plain words — Rückgängig for admins, on Laib's page.
func kept(ready laib.Readiness, admin bool, cfg BoxConfig) string {
	at, _ := time.Parse(time.RFC3339, ready.LastBackup.At)
	out := texte.T("box.keine-sicherung")
	if ready.LastBackup.OK {
		out = fmt.Sprintf(texte.T("box.gesichert"), when(at), texte.Anzahl(ready.LastBackup.Repos, "Ort", "Orten"))
	}
	if c := ready.LastChange; c.Summary != "" {
		changed, _ := time.Parse(time.RFC3339, c.At)
		out += " · " + fmt.Sprintf(texte.T("box.geaendert"), html.EscapeString(c.Summary), when(changed))
		if admin && c.Reversible {
			out += " " + laibLink(cfg, "/inbox", "Rückgängig")
		}
	}
	return out
}
