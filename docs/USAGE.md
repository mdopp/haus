# What the family uses — the usage counts (haus#50)

Back to the [README](../README.md) · the pages: [REFERENCE.md](REFERENCE.md).

Homes counts, server-side and anonymously, what the family uses, so that
what gets built next is decided on numbers (owner decision 2026-10-16):
a count per event and **UTC day**, nothing else. Never who: no
`Remote-User`, no device token, no IP, no content, no search words, no
entity — a request's query is read only for the widget source tag below.
The counts go to **usage-metrics** (mdopp/usage-metrics, a Laib built-in,
`POST /ingest`, app `haus`: the counted history's key, kept) with `HOMES_USAGE_URL` and `HOMES_USAGE_TOKEN`
(laib's `templates/homes` wires both; unset, Homes counts nothing and the
Box card is absent). Counting is a map increment in memory; a flush every
five minutes sends one increment per counter, and SIGTERM sends what is
left before Homes exits. usage-metrics not answering drops that batch and
says so once in the log; a page never waits for it.

| Event | Counted when | The card says |
|---|---|---|
| `seite.<page>` | a page is shown (GET, 200; behind the proxy secret): `start`, `raeume`, `ding` (a thing's page), `energie`, `medien`, `kalender`, `erinnerung`, `kontakt`, `dokument`, `notizen`, `notiz`, `sprechen`, `mehr`, `box`, `geraete`, `koppeln`; Start's *Braucht dich* fetched later (`/start/box`) is no view, its plain line counts for `start`; a script's words are not lines | *Start geöffnet*, … |
| `schalten.<domain>.<service>`, `schalten.light.dimmen`, `schalten.alle-lichter-aus` | a page's switch past its guards (`light.turn_on` … `cover.set_cover_position`; a dimmer's slider is `light.dimmen`) | *Licht ausgeschaltet*, *Licht gedimmt*, *Rollladen hochgefahren*, *Alle Lichter ausgeschaltet*, … |
| `stern.an`, `stern.aus` | the ☆ past its guards | *Ein Gerät auf Start geheftet (☆)* |
| `suche.notizen` | a search on Notizen (never its words) | *In den Notizen gesucht* |
| `sprechen.senden` | a sentence sent in the Sprechen field (never what) | *Im Sprechen-Feld geschrieben* |
| `sprechen.mikrofon` | speech turned into words for the field (never what) | *Ins Mikrofon gesprochen* |
| `gestoert.<page>.<line>` | a page showed a plain line because something did not answer: `licht`, `raeume`, `kalender`, `kontakte`, `notizen`, `bedeutung`, `dokumente`, `favoriten`, `favoriten-verdeckt`, `strom`, `strom-heute`, `verlauf`, `lautsprecher`, `schalten`, `bote`, `bote-langsam`, `box`, `laib`, `github`, `dienste` | the line itself and where: *„Licht und Geräte antworten gerade nicht.“ auf Start* |
| `napi.<path>` | a paired phone's `/napi` request, by path without wildcards (`napi.portal.state`, `napi.box.home`, `napi.ha.call`, …) | *Geräte-Widget aktualisiert*, … |
| `von.widget.<kind>` | a page opened from a widget, `?von=widget.<kind>` (homes-android#181): `device`, `devices`, `active`, `energy`, `energy-stats`, `energy-totals`, `box-overview`, `box-services`, `tool`; any other value counts nothing | *Vom Widget „Gerät“ aus geöffnet* |

**The voice control** (Home Assistant Assist), app `stimme`, when HA is
configured: HA keeps the last ten runs of each Assist pipeline in memory
(its debug store, `assist_pipeline/pipeline_debug/list|get` over the
WebSocket); Homes reads them every minute and counts each run once:
`erkannt` (HA's own intents answered), `weitergereicht` (the fallback
conversation agent answered), `nicht-erkannt` (the answer was an error,
e.g. `no_intent_match`). The sentence of a `nicht-erkannt` run goes to
usage-metrics' `POST /miss` (source `stimme`) — the words alone, never the
device, satellite or conversation; usage-metrics keeps it 30 days. Runs
from before Homes started are not counted, so a restart counts nothing
twice; more than ten commands on one pipeline within a minute lose the
oldest. **These commands are admin-only in HA**: the user behind
`HOMES_HA_TOKEN` must be an administrator, else Homes logs it once, counts
no voice, and the card says so.

**The Box page's card "Was wird genutzt"** (admins only): the last 30
days from usage-metrics' `GET /summary?days=30` and `GET /misses?days=30` —
the five most used things, what this box has and nobody or hardly anybody
used (*nie*), which plain lines the family saw where, the Bote's turns,
unanswered turns and most used tools (app `bote`: `zug`, `unbeantwortet`,
`werkzeug.<server>.<tool>` and Hermes' own `werkzeug.<tool>`, counted by
laib's Bote template, laib#275; shown as what was done, never a tool's
name: *Termine nachgesehen*, *Geräte ausgeschaltet*, *Gedächtnis*; a tool
not known yet as *Sonst etwas im Haushalt* or *Ein anderes Werkzeug*) and the voice's
numbers, and the sentences nobody could answer (*im Chat*, *per Stimme*;
kept 30 days, without names). usage-metrics not answering is one plain
line; nothing counted yet says so and since when Homes counts.

Before: ServiceBay's Solaris widgets tagged their links `?src=widget.*`
and the Solaris engine forwarded those hits to usage-metrics
(solarisbay#1026), nothing else was counted. Now the app only tags the
link (`?von=`), and Homes counts server-side itself — pages, actions,
degraded lines, widget refreshes and the voice — under the same store and
retention.

