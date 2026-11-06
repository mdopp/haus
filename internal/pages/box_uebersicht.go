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
	var found []laib.Finding
	var waiting []laib.Proposal
	var reports []laib.Entry
	var err, derr, ierr, rerr error
	fetchCtx(ctx, func(ctx context.Context) { ready, rows, err = cfg.Laib.Status(ctx) },
		func(ctx context.Context) { found, derr = cfg.Laib.Diag(ctx) },
		func(ctx context.Context) { waiting, ierr = cfg.Laib.Inbox(ctx) },
		func(ctx context.Context) { reports, rerr = cfg.Laib.JournalLatest(ctx, 1, "report") })
	if err != nil {
		return "<h1>Box</h1><p class=rot>" + saysLaibSilent + "</p>" + einzelheiten(err.Error())
	}
	lines := map[string][]string{} // by class: rot, gelb, k
	add := func(class, what string) {
		lines[class] = append(lines[class], "<li>"+dot(class, cmp.Or(map[string]string{"k": "Hinweis"}[class], class))+"<span class=was>"+what+"</span></li>")
	}
	shown := map[string]bool{}
	for _, s := range rows {
		if c := cls(s.State); c != "gruen" {
			shown[s.Name] = true
			add(c, serviceLabel(s)+aboutIt(s.Detail))
		}
	}
	for _, f := range found {
		if (f.Severity == "rot" || f.Severity == "gelb") && !shown[f.Service] {
			add(f.Severity, "<b>"+html.EscapeString(cmp.Or(f.Say, f.Probe))+"</b>"+aboutIt(f.Detail))
		}
	}
	switch {
	case errors.Is(derr, laib.ErrNoTool): // without diag, its red lines in laib status (laib#856)
		for _, p := range ready.Problems {
			var line string
			json.Unmarshal(p, &line)
			add("rot", aboutIt(line))
		}
		add("k", texte.T("box.diag-fehlt"))
	case derr != nil:
		add("gelb", texte.T("box.diag-stumm"))
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
	top := "<p><b class=gruen>" + texte.T("box.ruhig") + "</b> · " + kept(ready, isAdmin(r), cfg) + "</p>"
	if all := append(append(lines["rot"], lines["gelb"]...), lines["k"]...); len(all) > 0 {
		top = "<ul class=zeilen>" + strings.Join(all, "") + "</ul><p class=k>" + kept(ready, isAdmin(r), cfg) + "</p>"
	}
	return "<h1>Box</h1>" + top + morningReport(reports, rerr) + "<p>" + laibLink(cfg, "/journal", "Das Journal auf der Laib-Seite") + "</p>"
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
