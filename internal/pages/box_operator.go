package pages

import (
	"context"
	"errors"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/laib"
)

// Was der Operator kann (owner 2026-10-27: „ich möchte irgendwo die Skills
// sehen können“; laib#839): each of the Operator's skills in a row — what
// it does, when it runs, who decides, its last run and what came of it,
// and its folder on GitHub. Read only: enabling and pausing are a human's
// on Laib's side.

const skillsRepo = "https://github.com/mdopp/laib/tree/main/skills/"

const (
	saysSkillsNoList = "Laib nennt Haus die Liste der Skills noch nicht; dafür braucht Haus die Freigabe „skill“ (ein Vorschlag in Laibs Eingang). Bis dahin stehen hier nur die Skills, die schon gelaufen sind."
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

func operatorCard(ctx context.Context, c *laib.Client, now time.Time, loc *time.Location) string {
	var b strings.Builder
	b.WriteString("<h2>Was der Operator kann</h2>")
	ready, _, serr := c.Status(ctx)
	runs := ready.OperatorWork.Skills
	skills, err := c.Skills(ctx)
	if err != nil {
		if !errors.Is(err, laib.ErrNoTool) && serr != nil {
			return b.String() + "<p class=rot>" + saysLaibSilent + "</p>" + einzelheiten(err.Error())
		}
		b.WriteString("<p class=k>" + html.EscapeString(saysSkillsNoList) + "</p>")
		skills = nil
		for name := range runs { // what ran is known by its name only
			skills = append(skills, laib.Skill{Name: name, Source: "builtin"})
		}
		sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	}
	if len(skills) == 0 {
		return b.String() + "<p class=k>" + html.EscapeString(saysSkillsNone) + "</p>"
	}
	b.WriteString("<ul class=zeilen>")
	for _, s := range skills {
		class, word := "gruen", skillStatus[s.Status]
		switch s.Status {
		case "paused":
			class = "k"
		case "refused":
			class = "rot"
		case "":
			class, word = "k", "unbekannt"
		}
		b.WriteString("<li>" + dot(class, word) + "<span class=was><b>" + html.EscapeString(plainSkill(s.Name)) + "</b>")
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
		if run, ok := runs[s.Name]; ok && !run.At.IsZero() {
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
		b.WriteString("</span></li>")
	}
	b.WriteString("</ul><p class=k>Ein- und ausschalten geht auf der Laib-Seite; Haus zeigt nur.</p>")
	return b.String()
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
