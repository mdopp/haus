package pages

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/texte"
)

// auslastungPath is Box's page „Auslastung“ (owner 2026-11-06): what the
// box carries now, from laib auslastung (laib#1076). Only its values are
// fetched again, every auslastungMS while the page is visible: no reload.
const auslastungPath = "/box/auslastung"

const auslastungMS = "5e3"

// auslastungScript fetches the part again in place; a failed fetch keeps
// the values and marks them old, the time beside them says since when.
var auslastungScript = `<script>(function(){if(window.homesAuslastung||!window.fetch)return;window.homesAuslastung=1;
setInterval(function(){var el=document.getElementById('box-auslastung');if(!el||document.hidden)return;
fetch('` + BoxPartPath + `?was=auslastung',{credentials:'same-origin'}).then(function(r){if(!r.ok)throw 0;return r.json()})
.then(function(j){if(j['box-auslastung']){el.innerHTML=j['box-auslastung'];el.removeAttribute('data-alt')}})
.catch(function(){el.setAttribute('data-alt','')})},` + auslastungMS + `)})()</script>`

// auslastungStyle: bars and rings in the house palette, calm below 85 %.
var auslastungStyle = `<style>.uhren{display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:10px}
.uhr{background:var(--card);border-radius:14px;padding:14px 10px;text-align:center}.uhr span{display:block;font-size:.85rem}
.ring{--c:var(--ok);width:92px;height:92px;margin:0 auto 8px;border-radius:50%;display:grid;place-items:center;background:conic-gradient(var(--c) calc(var(--p)*1%),var(--track) 0)}
.ring b{width:72px;height:72px;border-radius:50%;background:var(--card);display:grid;place-items:center;font-size:1.1rem;font-variant-numeric:tabular-nums}
.balken{display:block;height:10px;border-radius:5px;background:var(--track);margin:6px 0;overflow:hidden}.balken i{display:block;height:100%;background:var(--ok)}
.balken.gelb i{background:var(--warn)}.balken.rot i{background:var(--danger)}.ring.gelb{--c:var(--warn)}.ring.rot{--c:var(--danger)}
[data-alt] .gemessen::after{content:" · ` + texte.T("auslastung.alt") + `"}</style>`

// auslastungCard is the page's part.
func auslastungCard(ctx context.Context, c *laib.Client) string {
	a, err := c.Auslastung(ctx)
	if errors.Is(err, laib.ErrNoTool) {
		return "<p class=gelb>" + texte.T("auslastung.kein-laib") + "</p>"
	}
	if err != nil {
		return "<p class=rot>" + saysLaibSilent + "</p>" + einzelheiten(err.Error())
	}
	var b strings.Builder
	b.WriteString("<h2>" + texte.T("auslastung.speicher") + "</h2><ul class=zeilen>")
	for _, d := range a.Disks {
		fmt.Fprintf(&b, "<li><span class=was><b>%s</b> <span class=k>%s</span><span class=\"balken %s\" role=img aria-label=\"%.0f %%\"><i style=\"width:%.1f%%\"></i></span><span class=text>%s</span></span></li>",
			texte.T(texte.ID("auslastung.platte."+d.Role)), html.EscapeString(d.Path), voll(d.Pct), d.Pct, d.Pct,
			fmt.Sprintf(texte.T("auslastung.belegt"), texte.Groesse(int64(d.Used)), texte.Groesse(int64(d.Size)), d.Pct, texte.Groesse(int64(d.Free))))
	}
	b.WriteString("</ul><p class=k>" + texte.T("auslastung.raid") + "</p><h2>" + texte.T("auslastung.rechner") + "</h2><div class=uhren>")
	b.WriteString(uhr(texte.T("auslastung.cpu"), a.CPU.Pct, fmt.Sprintf(texte.T("auslastung.last"), komma(a.CPU.Load[0]), komma(a.CPU.Load[1]), komma(a.CPU.Load[2]), a.CPU.Cores)))
	b.WriteString(uhr(texte.T("auslastung.ram"), anteil(a.Mem), vonGroesse(a.Mem)))
	if g := a.GPU; g != nil {
		b.WriteString(uhr(texte.T("auslastung.gpu"), float64(g.Pct), vonGroesse(laib.Belegt{Total: g.MemTotal, Used: g.MemUsed})))
	}
	b.WriteString("</div><p>" + texte.T("auslastung.swap") + " " + vonGroesse(a.Swap) + " · " + fmt.Sprintf(texte.T("auslastung.seit"), dauer(time.Duration(a.Uptime)*time.Second)) + "</p>")
	if g := a.GPU; g != nil {
		fmt.Fprintf(&b, "<h2>%s</h2><p>%s · %s</p><p>", texte.T("auslastung.gpu"), html.EscapeString(g.Name), fmt.Sprintf(texte.T("auslastung.watt"), g.Power, g.PowerLimit))
		if g.Holder == "" {
			b.WriteString(fmt.Sprintf(texte.T("auslastung.haushalt"), html.EscapeString(g.Model)))
		} else {
			b.WriteString(fmt.Sprintf(texte.T("auslastung.gehalten"), html.EscapeString(g.Holder), html.EscapeString(g.Window)))
		}
		b.WriteString("</p>")
	}
	b.WriteString("<h2>" + texte.T("auslastung.temperaturen") + "</h2><ul class=zeilen>")
	for _, t := range a.Temps {
		name, warm := texte.T("auslastung.temp.cpu"), 80.0
		if t.Name != "cpu" {
			name, warm = fmt.Sprintf(texte.T("auslastung.temp.platte"), html.EscapeString(t.Name)), 55
		}
		b.WriteString(grad(name, t.C, warm))
	}
	if g := a.GPU; g != nil {
		b.WriteString(grad(texte.T("auslastung.gpu"), float64(g.Temp), 83))
	}
	b.WriteString("</ul>")
	for _, m := range a.Missing {
		m, _, _ = strings.Cut(m, " (") // the reason is Laib's, for an agent
		b.WriteString("<p class=k>" + texte.T("auslastung.fehlt") + " " + html.EscapeString(m) + "</p>")
	}
	fmt.Fprintf(&b, "<p class=\"k gemessen\">"+texte.T("auslastung.gemessen")+"</p>", a.At.Local().Format("15:04:05"))
	return b.String()
}

// voll is a share's colour: calm, from 85 % yellow, from 95 % red.
func voll(pct float64) string {
	switch {
	case pct >= 95:
		return "rot"
	case pct > 85:
		return "gelb"
	}
	return ""
}

func uhr(name string, pct float64, unter string) string {
	return fmt.Sprintf("<div class=uhr><div class=\"ring %s\" style=\"--p:%.0f\" role=img aria-label=\"%s %.0f %%\"><b>%.0f %%</b></div><span>%s</span><span class=k>%s</span></div>", voll(pct), pct, name, pct, pct, name, unter)
}

func anteil(u laib.Belegt) float64 {
	if u.Total == 0 {
		return 0
	}
	return 100 * float64(u.Used) / float64(u.Total)
}

func vonGroesse(u laib.Belegt) string {
	return fmt.Sprintf(texte.T("auslastung.von"), texte.Groesse(int64(u.Used)), texte.Groesse(int64(u.Total)))
}

// grad is one temperature: yellow from warm, red ten degrees above.
func grad(name string, c, warm float64) string {
	class := map[bool]string{true: "gelb", false: ""}[c >= warm]
	if c >= warm+10 {
		class = "rot"
	}
	return fmt.Sprintf("<li><span class=was>%s</span><b class=\"k %s\">%.0f °C</b></li>", name, class, c)
}

func komma(v float64) string { return strings.Replace(fmt.Sprintf("%.2f", v), ".", ",", 1) }

func dauer(d time.Duration) string {
	if d >= 48*time.Hour {
		return fmt.Sprintf(texte.T("auslastung.tage"), int(d.Hours()/24))
	}
	return fmt.Sprintf(texte.T("auslastung.stunden"), int(d.Hours()))
}
