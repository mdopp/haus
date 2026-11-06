package pages

import (
	"cmp"
	"context"
	"errors"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/texte"
)

// Was der Operator kann (owner 2026-10-27: „ich möchte irgendwo die Skills
// sehen können“; laib#839): each of the Operator's skills in a row — what
// it does, when it runs, who decides, its last run and what came of it,
// and its folder on GitHub. Read only: enabling and pausing are a human's
// on Laib's side.

const skillsRepo = "https://github.com/mdopp/laib/tree/main/skills/"

const (
	saysSkillsNoList = "Laib nennt Homes die Liste der Skills noch nicht; dafür braucht Homes die Freigabe „skill“ (ein Vorschlag in Laibs Eingang). Bis dahin stehen hier nur die Skills, die schon gelaufen sind."
	saysSkillsNone   = "Der Operator hat noch keinen Skill."
)

var skillModell = map[string]string{
	"frei":   "Das Modell löst das Problem selbst.",
	"urteil": "Ein Skript sammelt, das Modell urteilt.",
	"nein":   "Nur ein Skript, kein Modell.",
}

var skillStatus = map[string]string{"active": "aktiv", "paused": "ruht", "refused": "verweigert"}

// skillOutcome is a run's end as laib#839's runner says it; another word shows as it comes
var skillOutcome = map[string]string{"green": "lief, alles gut", "unchanged": "lief, nichts Neues", "skipped": "übersprungen",
	"escalated": "an dich weitergegeben", "rejected": "abgewiesen", "error": "schiefgegangen"}

// skillGood are the ends of a run that need nobody; the others show first.
var skillGood = map[string]bool{"green": true, "unchanged": true, "skipped": true}

var weekdayWords = map[string]string{"Mo": "montags", "Di": "dienstags", "Mi": "mittwochs", "Do": "donnerstags", "Fr": "freitags", "Sa": "samstags", "So": "sonntags"}

var everyRE = regexp.MustCompile(`^(\d+)([mh])$`)

// skillWhen is a trigger in the family's words.
func skillWhen(s laib.Skill) string {
	t := s.Trigger
	switch {
	case t.Every == "${TICK}":
		return "regelmäßig, im Takt des Operators"
	case t.Every != "":
		if m := everyRE.FindStringSubmatch(t.Every); m != nil {
			unit := map[string]string{"m": "Minuten", "h": "Stunden"}[m[2]]
			if m[1] == "1" {
				unit = map[string]string{"m": "Minute", "h": "Stunde"}[m[2]]
				return "jede " + unit
			}
			return "alle " + m[1] + " " + unit
		}
		return "alle " + t.Every
	case t.Daily == "${REPORT_AT}":
		return "täglich zur Zeit des Morgenberichts"
	case t.Daily != "":
		return "täglich " + t.Daily
	case t.Weekly != "":
		day, at, _ := strings.Cut(t.Weekly, " ")
		if w := weekdayWords[day]; w != "" {
			return w + " " + at
		}
		return t.Weekly
	case t.Event == "red":
		return "wenn ein Dienst rot wird"
	case t.Event != "":
		return "bei „" + t.Event + "“"
	}
	return ""
}

// plainSkill is a skill's name as a word: „routineblick“ → „Routineblick“.
func plainSkill(name string) string {
	n := strings.ReplaceAll(name, "-", " ")
	if n == "" {
		return n
	}
	return strings.ToUpper(n[:1]) + n[1:]
}

// operatorCard is the Übersicht's Operator, folded (owner 2026-11-06):
// its state, then only the skills whose last run was not good; „Alle
// Skills zeigen“ shows the others.
func operatorCard(ctx context.Context, c *laib.Client, now time.Time, loc *time.Location) string {
	ready, _, serr := c.Status(ctx)
	runs := ready.OperatorWork.Skills
	b := strings.Builder{}
	b.WriteString("<details id=operator><summary>Operator: <span class=" + operatorClass(ready.Operator) + ">" + html.EscapeString(cmp.Or(ready.Operator, "unbekannt")) + "</span></summary>")
	skills, err := c.Skills(ctx)
	if err != nil {
		if !errors.Is(err, laib.ErrNoTool) && serr != nil {
			return b.String() + "<p class=rot>" + saysLaibSilent + "</p>" + einzelheiten(err.Error()) + "</details>"
		}
		b.WriteString("<p class=k>" + html.EscapeString(saysSkillsNoList) + "</p>")
		skills = nil
		for name := range runs { // what ran is known by its name only
			skills = append(skills, laib.Skill{Name: name, Source: "builtin"})
		}
		sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	}
	if len(skills) == 0 {
		return b.String() + "<p class=k>" + html.EscapeString(saysSkillsNone) + "</p></details>"
	}
	var rows []string
	allGood := true
	for _, s := range skills {
		run, ran := runs[s.Name]
		good := s.Status != "refused" && (!ran || run.Outcome == "" || skillGood[run.Outcome])
		allGood = allGood && good
		rows = append(rows, skillRow(s, run, ran, good, now, loc))
	}
	if allGood {
		b.WriteString("<p class=k>" + texte.T("operator.alle-gut") + "</p>")
	}
	b.WriteString("<input type=checkbox id=alle-skills><label for=alle-skills>" + texte.T("operator.alle-zeigen") + "</label><ul class=zeilen>" + strings.Join(rows, "") +
		"</ul><p class=k>Ein- und ausschalten geht auf der Laib-Seite; Homes zeigt nur.</p></details>")
	return b.String()
}

// skillRow is one skill: what it does, when it runs, its last run.
func skillRow(s laib.Skill, run laib.SkillRun, ran, good bool, now time.Time, loc *time.Location) string {
	var b strings.Builder
	class, word := "gruen", skillStatus[s.Status]
	switch s.Status {
	case "paused":
		class = "k"
	case "refused":
		class = "rot"
	case "":
		class, word = "k", "unbekannt"
	}
	b.WriteString(map[bool]string{true: "<li class=gut>", false: "<li>"}[good] + dot(class, word) + "<span class=was><b>" + html.EscapeString(plainSkill(s.Name)) + "</b>")
	if d := oneLine(s.Description); d != "" {
		b.WriteString(`<span class=text>` + html.EscapeString(d) + `</span>`)
	}
	var facts []string
	if w := skillWhen(s); w != "" {
		facts = append(facts, w)
	}
	if m := skillModell[s.Modell]; m != "" {
		facts = append(facts, m)
	}
	if s.Status == "paused" {
		facts = append(facts, "ruht gerade")
	}
	if len(facts) > 0 {
		b.WriteString(`<span class=text>` + html.EscapeString(strings.Join(facts, " · ")) + `</span>`)
	}
	if s.Status == "refused" && oneLine(s.Refused) != "" {
		b.WriteString(`<span class="text rot">Laib lässt ihn nicht laufen: ` + html.EscapeString(oneLine(s.Refused)) + `</span>`)
	}
	if ran && !run.At.IsZero() {
		b.WriteString(`<span class=wann>Zuletzt ` + html.EscapeString(whenWords(run.At, now, loc)))
		if o := skillOutcome[run.Outcome]; o != "" {
			b.WriteString(": " + html.EscapeString(o))
		} else if run.Outcome != "" {
			b.WriteString(": " + html.EscapeString(run.Outcome))
		}
		if w := oneLine(run.Why); w != "" {
			b.WriteString(" (" + html.EscapeString(w) + ")")
		}
		if c := oneLine(run.Caused); c != "" {
			b.WriteString(". " + html.EscapeString(c))
		}
		b.WriteString(`</span>`)
	} else {
		b.WriteString(`<span class=wann>Noch nicht gelaufen</span>`)
	}
	if s.Source != "state" {
		b.WriteString(`<span class=text><a href="` + skillsRepo + html.EscapeString(s.Name) + `" target=_blank rel=noopener>Der Skill auf GitHub</a></span>`)
	} else {
		b.WriteString(`<span class=text>Ein eigener Skill dieser Box</span>`)
	}
	return b.String() + "</span></li>"
}

// whenWords is a moment as „heute, 06:30“ or the day's words.
func whenWords(t, now time.Time, loc *time.Location) string {
	t = t.In(loc)
	day := dayWords(t, loc)
	if y, m, d := now.In(loc).Date(); t.Year() == y && t.Month() == m && t.Day() == d {
		day = "heute"
	}
	return day + ", " + t.Format("15:04")
}
