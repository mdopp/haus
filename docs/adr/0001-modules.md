# ADR 0001 — Homes is built from modules, and the app asks what the box can do

Applies when you add an area to Homes (a page, a nav entry, a Start part, a
`/napi` route, a widget type, a share target) or a `HOMES_*` variable; when
the app should show or hide something depending on the box; when a box
should run Homes without one of its areas; or when someone asks for a
second backend for an area (Nextcloud instead of Radicale, Plex instead of
Jellyfin).

- **Status:** Accepted, 2026-11-04. The plan; nothing is built yet. The
  steps are issues (below), done in the order given. Amended 2026-11-04
  (below, §10): no function is hidden. An area this box lacks leaves a
  quiet trace with „Hier anfordern“, and the trace shows the wish's state.
- **Deciders:** the owner (mdopp), 2026-11-04: „sollte es nicht ein Plugin
  sein, so dass man in anderen Installationen mehr/andere Tools nutzen
  kann; dass man auswählen kann, was der Server und damit die App kann und
  womit sie es macht“. The direction was accepted the same day: modules
  compiled in with an explicit list, no runtime plugins. The app reads
  what the server has, and Laib chooses.
- **Relates to:** laib ADR 0004 (templates: `${VAR}`, no logic), 0014
  (three programs), 0017 (data in standard backends), 0022 (the app is a
  widget client), 0026 (household verbs: values proposals).

## Context

Homes already switches about twenty areas on and off. It does this
implicitly, by asking whether a backend is set:

- About 60 `HOMES_*`/`LAIB_*` variables are read in `cmd/homes/main.go`
  and in a dozen `FromEnv` functions (`haushalt`, `ha`, `abs`, `jellyfin`,
  `reitti`, `samba`, `bote`, `usage`, `werkstatt`, `discogs`, `judge`,
  `energie`). Each one leaves a pointer nil or a string empty.
- `pages.StartConfig` (`internal/pages/start.go`) has about twenty fields
  documented as „nil: no page“. `shows()` in `internal/pages/family.go`
  turns them into the nav, and the hard-coded `tabs`, `haushaltPages` and
  `mehr` tables give each entry its place and icon.
- `routes()` in `cmd/homes/main.go` mounts the pages, partly behind
  `if s.x != nil`. `napi.Handler` (`internal/napi/napi.go`) mounts the
  `/napi` groups the same way.
- A missing backend is answered in four different ways. A page is either
  missing from the nav, says `notSet`, or is a 404. A `/napi` path is
  either missing (404 `not_found`) or answers 503 with a sentence
  (`sicherung`, `standort`, `gemeinsam`, `kontakte`).

**Nothing lists which areas exist.** You have to read `main.go`,
`StartConfig`, `shows()` and `napi.Handler` together to know what an area
is. The app cannot ask at all. homes-android hard-codes its widget types
(`WidgetTypes.ALL`), its Mehr rows (`HomeActivity.renderMehr`), its share
targets (manifest), its account parts and its backup parts. It finds a
missing area only when the person uses it and gets the error.

On the Laib side, a template has no conditionals (laib ADR 0004), and a
value is flat text. Homes learns that a backend exists only because that
service's post-deploy minted a token for it (HA, ABS, Jellyfin), or
because someone typed a URL into `values.yml` (`REITTI_URL`,
`GESUNDHEIT_URL`).

## Decision

### 1. A module is a Go value, compiled in; the list is explicit

There are no runtime plugins. Go's `plugin` package needs the same
toolchain and the same versions of every dependency, does not work on a
scratch image, and cannot unload. A plugin process per area would bring
its own API, its own auth, its own health and its own release, which
multiplies everything Laib watches. Every area stays in the one binary,
but each one *declares itself*. Homes builds one list from those
declarations, and everything else reads that list: the routes, the nav,
Start, the log line, `/napi/kann` and the Box page.

Homes' import rules (`scripts/loc.sh`) decide where the code goes:

- **`internal/modul`** holds the types and the list. It goes in the
  *shared* area and imports only the standard library.
- **`internal/napi`** and **`internal/pages`** export one route group per
  area, `func Standort(c Config) []modul.Route`, in place of today's
  `if c.Reitti != nil { … routes(mux, auth) }`. Both may import shared
  packages.
- **`cmd/homes/module.go`** is the catalog. It has one builder per
  module, because only `cmd` may import `pages`. A builder reads its
  variables, builds its connection, and returns the module, either on
  with handlers or off with a reason.

```go
// Package modul is what one area of Homes says about itself (ADR 0001):
// what it is called, what it talks to, what it serves and what it offers
// the app. Homes builds one Liste from them at start; it never changes
// while Homes runs. Standard library only (scripts/loc.sh: shared).
package modul

// Ort is where a module's entry sits in the family's selection.
type Ort uint8

const (
	Nirgends Ort = iota // no entry: a field, a card or a part of another page
	Leiste              // a tab of its own in the bar (Geräte, Energie)
	Haushalt            // a pill under the Haushalt tab (Kalender, Einkauf, Notizen)
	Mehr                // an entry under Mehr (Dokumente, Podcasts, …)
)

// Eintrag is one entry of the family's selection.
type Eintrag struct {
	Titel, Pfad, Icon string
	Ort               Ort
	Rang              int                      // its place at Ort among the core's and the modules' entries (H6)
	Fuer              func(*http.Request) bool // who sees it (signed in, admins); nil: everyone
}

// Route is one path a module serves: a page behind the proxy secret, or a
// /napi path the Liste puts behind the paired device's bearer.
type Route struct {
	Muster  string // a ServeMux pattern: "GET /napi/standort", "/standort"
	Handler http.Handler
}

// Angebot is what a module offers the app, in the app's keys
// (GET /napi/kann "app"; the keys are listed in ADR 0001 §2).
type Angebot struct {
	Widgets   []string // "geraet", "energie", "tool:note"
	Teilen    []string // share targets: "merken", "dokument", "standort-import"
	Konto     []string // the Homes account's parts: "kontakte", "kalender"
	Sicherung []string // the backup's parts: "whatsapp", "signal", "sms", "kontakte"
	Mehr      []string // the app's own Mehr rows: "daten-sichern", "standortverlauf"
}

// Modul is one area of Homes.
type Modul struct {
	Name     string   // the key: /napi/kann, HOMES_MODULE_AUS, the log. Never renamed.
	Titel    string   // German, as the family reads it: "Standortverlauf"
	Backend  string   // the Laib template it talks to: "reitti"; "" for Homes' own folder
	Standard string   // the open protocol, where there is one: "caldav", "carddav"
	Env      []string // the variables it reads (the catalog test holds every HOMES_* to one module)
	Braucht  []string // modules it needs on: energie needs geraete

	Seiten []Route   // pages, behind the proxy secret
	Napi   []Route   // /napi paths, behind the device's bearer
	Nav    []Eintrag // its entries in the family's selection
	Start  []string  // Start's parts it fills: "im-haus", "meine-geraete", "heute"
	App    Angebot
	Laeuft []func(context.Context) // its background work, started only while it is on

	// Grund is why it is off, one German sentence; "" while it is on.
	Grund string
	// Warum is the machine's word for Grund: "fehlt" (the box does not run
	// its backend) or "aus" (switched off in Laib although it does).
	Warum string
	// Aus is what its paths answer while it is off, only where a released
	// app relies on that answer (today: a 503 with a sentence for
	// sicherung, standort, gemeinsam, kontakte) or a page says notSet.
	// Every other path of an off module is the 404 of its surface.
	Aus []Route
}

// Liste is the box's modules in catalog order, on and off.
type Liste struct{ alle []Modul }

func Bauen(ms []Modul) (*Liste, error) // checks: unique names, Braucht on, no pattern twice
func (l *Liste) An(name string) bool
func (l *Liste) Eintraege(r *http.Request, o Ort) []Eintrag
func (l *Liste) Seiten(mux *http.ServeMux)                               // Seiten of those on, Aus of those off
func (l *Liste) Napi(mux *http.ServeMux, device func(http.HandlerFunc) http.HandlerFunc)
func (l *Liste) Starten(ctx context.Context)                             // each Laeuft of those on
func (l *Liste) Kann() Kann                                              // GET /napi/kann
func (l *Liste) String() string                                          // the start log line
```

A builder in `cmd/homes/module.go` looks like this. The example is the
smallest whole module of today:

```go
// standort is the Standortverlauf (owner 2026-11-02): the app's positions
// and imports into the person's Reitti, the page with the person's token.
func standort(b *bau) modul.Modul {
	m := modul.Modul{Name: "standort", Titel: "Standortverlauf", Backend: "reitti",
		Env: []string{"HOMES_REITTI_URL", "HOMES_REITTI_PUBLIC_URL"}}
	r := reitti.FromEnv(b.dataDir)
	if r == nil {
		return b.fehlt(m, "Auf dieser Box läuft kein Reitti.", napi.StandortAus()...)
	}
	if b.aus(m.Name) { // HOMES_MODULE_AUS
		return b.ausgeschaltet(m, napi.StandortAus()...)
	}
	b.family.Reitti = r // the only way a page learns of it: off, the field stays nil
	m.Seiten = pages.StandortSeiten(&b.family)
	m.Napi = napi.Standort(r, b.whoami)
	m.Nav = []modul.Eintrag{{Titel: "Standortverlauf", Pfad: pages.StandortPath, Ort: modul.Mehr,
		Icon: pages.IconStandort, Fuer: pages.Angemeldet}}
	m.App = modul.Angebot{Teilen: []string{"standort-import"}, Mehr: []string{"standortverlauf"}}
	return m
}
```

`main` reduces to: read the core (Laib, the proxy secret, the data
folder), run every builder in catalog order, call `modul.Bauen`, log
`l.String()` („Module an: geraete (home-assistant), kalender (radicale),
…; aus: standort (fehlt: kein Reitti), podcasts (aus)“), then
`l.Starten`, `l.Seiten`, `l.Napi`. The **core** is always on and is no
module: Start's frame and box line, Mehr, Aufgaben, Box (Einladen,
Geräte, pairing), Willkommen, Adresse, Abmelden, the Papierkorb, font and
icon, `/napi/{assistent,app,persona,whoami,kann}`, the Box keys under
`/napi/box`, and `/mcp/haushalt` (its tools come from the modules that
are on).

**The rule that makes „off“ true:** a page learns of a module only through
the `StartConfig` field its builder sets. An off module's field stays nil,
so every existing `cfg.X != nil` check keeps working. Its routes are not
mounted, its nav entries are not in the list, its background work does
not start, and its Bote tools are not in `haushalt.Tools`.

**As built (homes#243):** the route groups are not fields of the
`Modul`. `napi.Gruppen(c)` (and, for the pages, `pages.Gruppen(cfg)`)
give each module's routes by its name, and Homes mounts them together
with the core's. The reason is that a page needs the `Liste` itself, for
its nav (H6), so the `Liste` cannot hold the pages' handlers. An off
module's group is what its paths answer without its backend. That is
`Aus` (§4) where a released app relies on it, and the surface's 404
everywhere else, so `Modul` has no `Seiten`, `Napi` or `Aus`.

### 2. `GET /napi/kann`: what this box can do, for the app

The endpoint needs a paired device (401 otherwise), like every `/napi`
path, so a stranger cannot learn which backends the box runs. The answer
carries an `ETag` and is a 304 on `If-None-Match`.

```json
{
  "version": 1,
  "module": [
    {"name": "geraete",  "titel": "Geräte",          "an": true,  "backend": "home-assistant"},
    {"name": "kalender", "titel": "Kalender",        "an": true,  "backend": "radicale", "standard": "caldav"},
    {"name": "standort", "titel": "Standortverlauf", "an": false, "backend": "reitti",
     "warum": "fehlt", "grund": "Auf dieser Box läuft kein Reitti."},
    {"name": "podcasts", "titel": "Podcasts",        "an": false, "backend": "audiobookshelf",
     "warum": "aus",   "grund": "Auf dieser Box ausgeschaltet."}
  ],
  "app": {
    "widgets":   ["geraet", "energie", "tool:note", "tool:doc"],
    "teilen":    ["merken", "dokument"],
    "konto":     ["kontakte", "kalender"],
    "sicherung": ["whatsapp", "signal", "sms", "kontakte"],
    "mehr":      ["daten-sichern"]
  }
}
```

`app` is the union of the `Angebot`s of the modules that are on. The app
reads the lists and never maps modules to features itself, so a server
can move a widget from one module to another without an app release. The
`module` list is for words: the Box page, the app's „gibt es hier nicht“
sentence, and support.

The keys the app knows today are:

| list | keys |
|---|---|
| `widgets` | `geraet`, `energie`, and `tool:<id>` for each tool def (`note`, `doc`, `contacts`, `task`) |
| `teilen` | `merken` (text), `dokument` (images), `standort-import` (GPX/GeoJSON), `musik` (songs and ZIPs) |
| `konto` | `kontakte`, `kalender` |
| `sicherung` | `whatsapp`, `signal`, `sms`, `kontakte` |
| `mehr` | `daten-sichern`, `standortverlauf` |

**Compatibility rules:**

- Fields and keys are only ever added. A key's meaning never changes. A
  change that cannot be made by adding becomes `version: 2`, and the
  server answers the old shape to an app that sends no `?version=2`.
- `/napi/defs/tool` stays as it is. It is the tool widgets' catalog and
  agrees with `widgets` by construction, because both come from the same
  modules.

### 3. What the app does with it

- **When it reads:** at app start and resume (at most once a minute,
  with `If-None-Match`); when the live stream connects again (a restarted
  Homes may have a new list); and in the account's hourly sync. The last
  answer is cached and the app works from it while offline.
- **An unknown key** (a newer server) is ignored. **A known key that is
  missing** means the box does not have it. Its Mehr row, its widget type,
  its account part and its backup part become a quiet trace (§10), never
  hidden; the share target is disabled, and its trace sits in Mehr.
- **An unknown module** in `module` is only shown in words.
- **The server has no `/napi/kann`** (404 `not_found`, an older Homes):
  the app behaves exactly as today and shows everything. The same applies
  when no answer has ever arrived. The app never hides a feature on a
  guess.
- **Share targets are static in the manifest.** Each one becomes its own
  component: an `activity-alias` per filter for `ShareActivity` (`merken`
  for text, `dokument` for images), plus `StandortImportActivity`. The
  app switches them with
  `PackageManager.setComponentEnabledSetting(…, DONT_KILL_APP)`, which is
  allowed for the app's own components with no permission. The dynamic
  Direct Share shortcut (`ShareTarget.kt`) follows the same keys. While
  `kann` is unknown, everything stays enabled, as today.
- **Widget providers** are disabled the same way, but only while none of
  that type is placed. Disabling a provider takes its placed widgets off
  the home screen. A placed widget whose type the box lost shows the
  module's `grund` in its body instead of an error.
- **Account parts:** a missing `konto` key stops that part's sync
  (`setIsSyncable(…, 0)`) and says why in `HomesKontoActivity`. It never
  deletes what is already on the phone without asking.

### 4. Older apps against a newer Homes

An app that never calls `/napi/kann` keeps working. Every path of a
module that is on answers exactly as before. A path of a module that is
off answers what it answers today with the backend unset: a 503 with the
sentence released apps show (`Modul.Aus`) for `sicherung`, `standort`,
`gemeinsam` and `kontakte`, and the 404 `not_found` everywhere else. The
one new behaviour is that a module switched off *in Laib* answers like
one whose backend is missing.

### 5. Laib chooses: derived from what the box runs, overridden by one value

- **The default comes from what the box runs.** A module is on when its
  backend's wiring is there. The wiring is the token the backend's
  post-deploy mints for Homes (HA, ABS, Jellyfin), the password the auth
  template seeds (Radicale), the folder the template mounts (notes,
  music inbox, archive inbox), or a value. This is today's behaviour,
  now explicit and logged. Backends whose address is still typed by hand
  (`REITTI_URL`, `GESUNDHEIT_URL`) should reach Homes the same way the
  token-minted ones do (laib issue below).
- **The override is one value:**
  `MODULE_AUS: ""  # podcasts,energie,…` in
  `templates/homes/values.example.yml`, rendered as `HOMES_MODULE_AUS`.
  - *One* key, because values are flat text, a template update appends
    keys, and a key per module would grow the file with every module.
  - An *off*-list, not an on-list, so that a new module in a Homes
    release appears on every box that runs its backend without a values
    change (as today), and an older box's list stays right.
  - An unknown name in the list is logged and ignored, never fatal: Laib
    and Homes release apart.
- **How the owner switches.** Homes' Box page (admins) lists the modules
  with their backend, on or off, and the reason. A button proposes the
  change: `laib propose --set services/homes/values.yml:MODULE_AUS=…`
  (laib ADR 0026). The owner approves it in Laib's inbox, and Homes
  restarts with the new list. Homes decides nothing (haus#1).
- **`uses:` names every backend.** The homes template lists all its
  backends, not only `home-assistant` and `reitti`, so `laib planned` and
  `laib net` explain a module that is off.
- **The backend choice comes later.** When a second backend for a module
  exists, choosing it is one more value per module
  (`KALENDER_BACKEND: radicale`) and one more builder branch. That is not
  built now (laib CLAUDE.md: no switch for a case that cannot happen).

### 6. Alternative backends: the seams, not built

Open standards already cover most of the household: CalDAV/CardDAV
(Radicale, also Nextcloud), Home Assistant as the hub for every device,
Markdown files for notes, and an OpenAI-compatible chat API for the Bote.
An interface is added only when a real second backend is wanted, at
these places:

| module | backend today | where the interface would go |
|---|---|---|
| kalender, kontakte, einkauf, konto | Radicale (`caldav.Client`) | likely none: Nextcloud speaks CalDAV/CardDAV. Collection discovery in `caldav` and the account in `haushalt.FromEnv` |
| dokumente | Paperless (`paperless.Client`) | `haushalt.Config.Documents`, `StartConfig.Docs`, `napi` tools: an `Ablage` {Suchen, Dokument, Ablegen} |
| gesehen, musik („hast du schon“) | Jellyfin | already interfaces: `gesehen.Filme`, `einkauf.Bibliothek` (Plex fits behind them) |
| podcasts | Audiobookshelf (`abs.Client`) | `podcasts.Service.ABS` |
| standort | Reitti | `napi.standort.r`, `pages.Standort` (Dawarich, OwnTracks Recorder) |
| geraete, energie | Home Assistant | none foreseen; HA is the integration layer |

### 7. The modules

All of today's optional areas, with what they talk to and read. „Radicale“
means `HOMES_RADICALE_URL`, `_USER`, `_PASSWORD` (+ `_CALENDAR`,
`_ADDRESSBOOK`, `_PUBLIC_URL` where named). Core variables, not in a
module: `LAIB_URL`, `LAIB_TOKEN`, `LAIB_PUBLIC_URL`, `LAIB_DOMAIN`,
`HOMES_PROXY_SECRET`, `HOMES_DATA`, `HOMES_LISTEN`, `HOMES_PUBLIC_URL`,
`HOMES_ASSISTANT`, `HOMES_HAUSHALT_TOKEN`, `HOMES_PHONE_COUNTRY`, `TZ`.

| module | Titel | backend (Laib template) | env | pages · nav | Start | /napi | app |
|---|---|---|---|---|---|---|---|
| `geraete` | Geräte | home-assistant | `HOMES_HA_URL`, `HOMES_HA_TOKEN`; opt. `HOMES_HA_PUBLIC_URL`, `HOMES_COVER_INVERTED` | `/raeume`, `/raeume/…`, `/medien`, `/schalten`, `/aktivierungswort`, `/live` · Leiste | im-haus, meine-geraete | `ha/call`, `portal/{state,active,entity-history,camera/…,start/addable,favorites,watch,events}` | widgets `geraet`; favourites |
| `energie` | Energie | home-assistant (needs `geraete`) | opt. `HOMES_ENERGY_FLOW`, `_TOTALS`, `_STATS` | `/energie` · Leiste | – | `portal/energy` | widgets `energie` |
| `sprechen` | Sprechen | bote | `HOMES_BOTE_URL`, `HOMES_BOTE_KEY`; opt. `HOMES_STT_PROVIDER`, `HOMES_STT_LANGUAGE` (HA), `HOMES_BOTE_SIGNAL` | `/sprechen`, `/sprechen/hoeren` · none | the field | – | – |
| `kalender` | Kalender | radicale (CalDAV) | Radicale, `_CALENDAR`, `_PUBLIC_URL` | `/kalender`, `/kalender/{verwalten,hinzufuegen,aendern,erinnerung/…}` · Haushalt | heute | `portal/tasks`, def `task` | widgets `tool:task`; Bote `termine`, `erinnerungen`, … |
| `kontakte` | Kontakte | radicale (CardDAV) | Radicale, `_ADDRESSBOOK` | `/kontakte/…` · none | – | `portal/contacts`, def `contacts`, `POST kontakte` | widgets `tool:contacts`; sicherung `kontakte` |
| `konto` | Homes-Konto | radicale | Radicale | – | – | `gemeinsam/{kontakte,kalender}`, live frame `gemeinsam` | konto `kontakte`, `kalender` |
| `einkauf` | Einkauf | radicale (VTODO lists) | Radicale | `/einkauf`, `/einkauf/rezepte` · Haushalt | – | – | Bote list tools |
| `notizen` | Notizen | file-share (folder); opt. qmd, the house model | `HOMES_NOTES_DIR`; opt. `HOMES_QMD_URL`, `_COLLECTION`, `HOMES_LLM_URL`, `_KEY`, `_MODEL` | `/notizen`, `/notizen/…` · Haushalt | – | `portal/notes`, def `note`, `POST merken` | widgets `tool:note`; teilen `merken` |
| `dokumente` | Dokumente | paperless | `HOMES_PAPERLESS_URL`, `_TOKEN`; opt. `_PUBLIC_URL`, `_LAN_ONLY`, `HOMES_UPLOAD_DIR`, `HOMES_DOCUMENTS_MAIL`, `HOMES_DOCUMENTS_DIR`, `HOMES_QMD_DOCUMENTS` | `/dokumente`, `/dokumente/…`, `/scannen`, `/scannen/trennblatt` · Mehr | – | `portal/documents`, def `doc`, `POST upload` | widgets `tool:doc`; teilen `dokument` |
| `musik` | Musik | music inbox (beets), radicale (wishes), MusicBrainz; opt. media (Jellyfin), Discogs, the house model | `HOMES_MUSIC_INBOX` (or the `/musik-eingang` mount); opt. `HOMES_JELLYFIN_URL`, `_TOKEN`, `HOMES_DISCOGS_KEY`, `_SECRET`, `HOMES_LLM_VISION_MODEL` | `/musik`, `/musik/bild`, `/discogs`, `/discogs/zurueck`, `/platte`, `/platte/bild` · Mehr | – | `POST musik` | teilen `musik` (with the inbox); Bote wish tools |
| `podcasts` | Podcasts | audiobookshelf | `HOMES_ABS_URL`, `HOMES_ABS_TOKEN` | `/podcasts` · Mehr | – | – | Bote podcast tool; new episodes told |
| `gesehen` | Gesehen & Gehört | media (Jellyfin) and/or audiobookshelf; radicale (wishes) | `HOMES_JELLYFIN_URL`, `HOMES_JELLYFIN_KEY`; `HOMES_ABS_*` | `/gesehen` · Mehr | – | – | – |
| `sicherung` | Daten sichern | archiv (each person's inbox) | `HOMES_ARCHIV_DIR`; opt. `HOMES_ARCHIV_INTERN`, `HOMES_ARCHIV_SECRET_<PERSON>` | `/daten-sichern`, `/nachrichten-sichern` · Mehr | – | `sicherung/*` (503 while off) | sicherung `whatsapp`, `signal`, `sms`; mehr `daten-sichern` |
| `standort` | Standortverlauf | reitti | `HOMES_REITTI_URL`; opt. `HOMES_REITTI_PUBLIC_URL` | `/standort` · Mehr | – | `standort`, `standort/import/*` (503 while off) | teilen `standort-import`; mehr `standortverlauf` |
| `netzlaufwerk` | Netzlaufwerk | file-share (Samba) | `HOMES_SAMBA_DIR`, `HOMES_SAMBA_HOST` | `/netzlaufwerk` · Mehr | – | – | – |
| `gesundheit` | Gesundheit | gesundheit (a link only) | `HOMES_GESUNDHEIT_URL` | the card on Mehr | – | – | – |
| `werkstatt` | Werkstatt | GitHub (admins) | `HOMES_WERKSTATT_REPOS`; opt. `HOMES_GITHUB_API` | the card on Box | – | – | – |
| `nutzung` | Was wird genutzt | usage-metrics (admins) | `HOMES_USAGE_URL`, `HOMES_USAGE_TOKEN` | Box's „Was wird genutzt“ | – | counts `/napi` | – |
| `fotos` | Fotos | immich (a link only, on while Laib runs it) | – | `/einrichten/fotos` | – | – | the Immich app's setup |
| `passwoerter` | Passwörter | vaultwarden (a link only) | – | `/einrichten/passwoerter` | – | – | the Bitwarden app's setup |
| `filme` | Filme und Musik | media (a link only) | – | `/einrichten/filme` | – | – | the Jellyfin app's setup |

Known couplings that the migration has to take care of:

- **One Radicale account serves five modules** (`kalender`, `kontakte`,
  `konto`, `einkauf`, and the wish lists in `musik`/`gesehen`). The
  connection is built once. Each builder decides on its own module, and
  `ensureHaushalt` makes only the collections of the modules that are on.
- **The live stream (`/napi/portal/events`) is mounted only with HA**,
  but it also carries `konto`'s `gemeinsam` frames. It moves to the core
  when `geraete` becomes a module (step H6).
- **`musik`'s Discogs and Platte entries appear on their own under Mehr
  only on a box without a music inbox** (`shows()`). That stays as an
  `Eintrag.Fuer` rule inside the module.

### 8. Tests

- **Every module off, one at a time** (`cmd/homes`, a table over the
  catalog). Build `fullSite` (every backend from its fake) with
  `HOMES_MODULE_AUS=<name>`, then check:
  - each pattern the module serves when on answers its `Aus` route or
    the surface's 404 (`Elsewhere`, `not_found`), never the handler;
  - none of its `Nav` titles appears on `/start` or `/mehr`;
  - `/napi/kann` lists it as `an:false`, and none of its `Angebot` keys
    is left in `app` unless another module that is on offers it;
  - every other module's routes still answer as with everything on.
- **Every declared route is served.** With everything on, each `Seiten`
  and `Napi` pattern of each module is mounted. The test fails when a
  pattern is declared but the route was never built.
- **Every variable has an owner.** A test greps the production code for
  `"HOMES_…"` and fails on a variable that is in no module's `Env` and
  not in the core list, so the table in §7 cannot go stale.
- **`modul.Bauen`** refuses a duplicate name, a pattern served twice, or
  a `Braucht` whose module is off.
- **`/napi/kann`:** 401 without a device; the shape (a golden JSON for
  `fullSite`); 304 on the ETag; an unknown name in `HOMES_MODULE_AUS` is
  logged and harmless.
- **During migration only:** for a set of env combinations, the nav that
  the list builds equals what `shows()` builds today. This test goes
  away with `shows()`.
- **App:** the `kann` parser handles unknown keys, missing keys, a 404
  (meaning „as today“) and the cache. An emulator test switches a share
  alias and checks it disappears from the share sheet.
- **Laib:** `laib template lint templates/homes` with `MODULE_AUS` set,
  and the rendered unit carries `HOMES_MODULE_AUS`.

### 9. Order, smallest risk first

| step | repo | issue | what changes for a person | estimate (production lines · PRs · risk) |
|---|---|---|---|---|
| H1 | homes | [homes#241](https://github.com/mdopp/homes/issues/241) | nothing; Homes logs its module list | +330 · 1 · low |
| H2 | homes | [homes#242](https://github.com/mdopp/homes/issues/242) | nothing; the app can ask | +90 · 1 · low |
| A1 | homes-android | [homes-android#231](https://github.com/mdopp/homes-android/issues/231) | Mehr rows and widget types follow the box | +350 · 1–2 · medium |
| H3 | homes | [homes#243](https://github.com/mdopp/homes/issues/243) | leaf modules can be switched off | +250/−120 · 1–2 · medium-low |
| L1 | laib | [laib#1036](https://github.com/mdopp/laib/issues/1036) | the owner's switch exists in `values.yml` | +30–150 · 1 · low |
| H4 | homes | [homes#244](https://github.com/mdopp/homes/issues/244) | dokumente, notizen, musik, einkauf too | +220/−150 · 1 · medium |
| H5 | homes | [homes#245](https://github.com/mdopp/homes/issues/245) | kalender, kontakte, konto too | +180/−120 · 1 · medium |
| H6 | homes | [homes#246](https://github.com/mdopp/homes/issues/246) | geraete, energie, sprechen too; the nav comes only from modules | +250/−280 · 1–2 · medium-high |
| A2 | homes-android | [homes-android#232](https://github.com/mdopp/homes-android/issues/232) | share targets follow the box | +160 · 1 · medium |
| A3 | homes-android | [homes-android#233](https://github.com/mdopp/homes-android/issues/233) | account and backup parts follow the box | +200 · 1 · medium-high |
| H7 | homes | [homes#247](https://github.com/mdopp/homes/issues/247) | the Box page lists modules and proposes a switch | +200 · 1 · low-medium |
| L2 | laib | [laib#1037](https://github.com/mdopp/laib/issues/1037) | Reitti and Gesundheit are found, not typed | +50–150 · 1 · medium |

Total: about 13–16 PRs, about 2,500 production lines touched (Homes
about 1,500 of them moves out of `main.go`, `family.go` and
`napi.Handler`; the app about 700; Laib about 200). Tests add about as
much again.

Why this order:

- H1 and H2 change no behaviour. They give the app something to read
  early, so A1 can start while the server side moves.
- H3 to H6 move areas from the ones with their own paths and backend
  (`gesundheit`, `werkstatt`, `netzlaufwerk`, `podcasts`, `gesehen`,
  `standort`, `sicherung`) to the ones that share a connection or fill
  Start (Radicale, then HA). HA comes last because Start, favourites and
  the live stream hang on it.

## Consequences

- An area is one builder plus its route groups. Adding one means adding
  a builder, and the tests fail until its routes, variables and nav are
  declared.
- Homes' start log and the Box page say what this box can do and why
  not. Today that is read off nil pointers.
- The app stops offering what the box cannot do, and it stops failing
  on first use.
- `cmd` and the shared area grow. `internal/modul` adds about 180 lines
  to *shared* (cap 1379, at 1336), and the catalog adds about 250 to
  *cmd* (cap 1069, at 1021), while about 100 move out of `main.go`'s
  `routes()`. Both caps are raised in the PR that crosses them, with the
  reason, and need the owner's yes (`scripts/loc.sh`).
- No runtime plugins, so an area from outside the repo still needs a PR
  to Homes. That is the cost of one binary, one image, one release, and
  one thing for Laib to watch.

## 10. Amendment 2026-11-04 — nothing is hidden: a trace, „Hier anfordern“, the wish's state

The owner, on the plan (2026-11-04): „Ich würde es ganz durchziehen. Aber
ich würde in der App und im Frontend kleine Spuren hinterlassen, was die
App kann. Gleich mit der Option ‚hier bitte anfordern‘, was dann ein Wish
anlegt. Und das auch im Link darstellen (wurde angefordert, wartet, wurde
abgelehnt, …). Keine Funktion soll versteckt werden.“

This replaces „hidden“ in §1 and §3. A module that is off keeps its routes
unmounted (§1, §4: its paths still answer 404 or its `Aus`), but it is not
silent: it leaves **a trace**, one quiet line where its entry would be.

### What a trace is

- **One muted line**: the module's title and where it stands, „Standortverlauf
  — auf dieser Box nicht eingerichtet“. Grey text and a grey icon in the
  house's palette; no red, no warning sign, no error words. It must read as
  „gibt es hier noch nicht“, never as something broken.
- **Its words, by `warum` and the wish:**

  | the module | the trace says |
  |---|---|
  | `fehlt`, nobody asked | „auf dieser Box nicht eingerichtet“ |
  | `aus` (the owner switched it off), nobody asked | „ausgeschaltet“ |
  | a wish, open | „angefordert“ |
  | a wish, a proposal for it in the owner's Inbox | „wartet auf Freigabe“ |
  | a wish, the proposal rejected, or the wish closed without it | „abgelehnt“ |
  | a wish, the proposal approved, the module not on yet | „wird eingerichtet“ |
  | the module is on | no trace: the module's own entry is there |

  „umgesetzt“ is the wish's last state (Homes closes it when the module
  comes on); a person never sees it on a trace, because the trace has
  given way to the module itself.
- **One action, „Hier anfordern“**, on every trace a signed-in person
  sees, also on „ausgeschaltet“ and again after „abgelehnt“. While a wish
  is open (angefordert, wartet, wird eingerichtet) the trace shows the
  state instead of the button. Pressing it answers at once and ends
  visibly (Homes CLAUDE.md): „angefordert“, or the reason in plain German
  when Laib refuses or does not answer.

### Where traces are

- **Web:** Mehr ends with a quiet card, „Gibt es hier noch nicht“, one
  trace per module that is off and has no admin-only entry. That covers
  modules with a Leiste or Haushalt entry too (Geräte, Kalender, …) and
  those without one (Sprechen, Kontakte, Homes-Konto). `werkstatt` and
  `nutzung` (admins) leave theirs on the Box page's „Was Homes kann“
  (H7), which lists every module.
- **A Mehr module's own page** (owner 2026-11-04, the Mehr audit): `/dokumente`,
  `/podcasts`, `/gesehen`, `/daten-sichern`, `/standort` and `/netzlaufwerk` on
  a box without the module are its trace with „Hier anfordern“ (`modulAus`),
  never „Nicht gefunden“ or a setting's name; the wish ends back on that page,
  and an admin gets the module's `/module/<name>`. A bookmark or an app's link
  lands on the trace, not on a dead end.
- **App:** its Mehr list has the same quiet section from `/napi/kann`.
  The widget picker shows a type the box lacks as a muted entry with its
  trace instead of leaving it out. The account and the backup show a
  missing part as a switch that is off and disabled, with the trace
  below. A share target cannot be shown disabled in Android's share
  sheet, so its component stays disabled (§3) and its trace is the
  module's line in Mehr.

### `/napi/kann` grows (version stays 1, §2's rules: only added)

Each entry of `module` gains:

- `bietet`: the module's `Angebot` in the keys of `app`, on or off, so the
  app knows which widget type, share target, account or backup part an
  off module would bring and puts its trace there. The app still never
  maps a module to a feature itself.
- `spur_id`: the trace's stable key for an off module (`fehlt`, `aus`,
  or the wish's state), absent while on; `spur`: its German words beside
  it („auf dieser Box nicht eingerichtet“, „ausgeschaltet“, …). The app
  says the key in its own language (owner 2026-11-04: Homes becomes
  multilingual); Homes' new words are one table, `internal/texte`.
- `admins`: `true` for a module only admins use (`werkstatt`,
  `nutzung`), whose trace is on the Box page only.
- `wunsch`, only while there is one: `{"stand": "angefordert" | "wartet" |
  "abgelehnt" | "eingerichtet" | "umgesetzt", "seit": "2026-11-04"}`.
- `anfordern`: `true` when the trace offers „Hier anfordern“.

`POST /napi/kann/anfordern` `{"modul": "<name>"}` files the wish for the
device's person and answers the module's new entry. 401 without a device;
404 for a module that is on or does not exist; a wish already open answers
200 with that one, never a second.

### The wish, through Laib

Homes holds no GitHub token and decides nothing (haus#1); the wish and its
state are Laib's (laib ADR 0026: household verbs live behind the verb
door, in laib-haushalt). The new verb is laib's issue
[laib#1038](https://github.com/mdopp/laib/issues/1038):

- **Filing:** `laib_wish` with `modul` and `von`. laib-haushalt files it
  the way the Bote's wish is filed (laib #883), as a Box-Aufgabe for the
  admins, never a GitHub issue (the family's words stay in the house, and
  issues are for program bugs only). Its title is „Wunsch aus Homes:
  <Titel>“. Who asked is its `von`. Its `weg` is the Homes page
  `/module/<name>`, where an admin sees the module, its backend and the
  switch (H7). The `weg` is also how Laib knows which module the wish is
  for.
- **Reading:** `laib_wish` with `stand` and the off modules' names with
  their backends answers the state of each one's latest wish. laib-haushalt
  joins the Box-Aufgabe with the owner's Inbox, which it reads as the
  `haushalt` hand. A proposal is „for“ a module when it adds the module's
  backend as a service, or when it changes `services/homes/values.yml`
  `MODULE_AUS` so that the module leaves the list. Only proposals filed
  since the wish count. An open one is `wartet`, a rejected one
  `abgelehnt` (its reason comes along), and an approved one
  `eingerichtet`. „Später“ is no answer and leaves the wish `angefordert`,
  as does a run that rolled the change back. A Box-Aufgabe ticked off by
  Homes itself is `umgesetzt`. One ticked off by a person while the
  module is still off is `abgelehnt`; the task's text says so to the
  admin.
- **Closing:** when Homes starts with a module on that has an open wish,
  it ticks the Box-Aufgabe off (`laib_aufgabe erledigt`, as the `homes`
  hand).
- Homes reads the states in one call, keeps them a minute, and shows the
  last known state, unmarked, while Laib does not answer. Without the
  verb (an older Laib) the trace has no button and says only
  „nicht eingerichtet“ or „ausgeschaltet“.

### Steps this adds

| step | repo | issue | what changes for a person |
|---|---|---|---|
| L3 | laib | [laib#1038](https://github.com/mdopp/laib/issues/1038) | a wish for a Homes module, and its state, through Laib |
| H8 | homes | [homes#251](https://github.com/mdopp/homes/issues/251) | Mehr shows what the box lacks, with „Hier anfordern“ and the state; `/napi/kann` carries `bietet`, `spur`, `wunsch`, `anfordern` |

H8 follows H2 and L3 and can go before H3: the modules that are `fehlt`
already exist on every box. A1, A2, A3 and H7 change with it:

- **A1:** the app's Mehr rows and the widget picker show traces with
  „Hier anfordern“ instead of hiding.
- **A2:** a disabled share target leaves its trace in Mehr.
- **A3:** account and backup parts show disabled with their trace.
- **H7:** the Box card shows each module's wish and its state, and
  `/module/<name>` is the page a wish's Box-Aufgabe leads to.

Weighed against the plan before this amendment: the family learns what
the box could do, and asks for it where they miss it, instead of a feature
that silently is not there. Maintenance gets no worse: the trace comes
from the same module list, and the wish is Laib's existing Box-Aufgabe
with one more read. What it costs: one more card on Mehr and one more call
to Laib per Mehr view (cached), and about 150 lines in laib-haushalt.

## 11. Amendment 2026-11-04 — each module carries its own setup

The owner, on the Willkommen page's guides: they went stale in one
place; the setup belongs with each function, and as a task per app.

- **`modul.Einrichtung`** on a module: the address to open, its own
  Android app (name, package, Play link) or none (web only), short steps
  as `internal/texte` ids, and the vendor's guide. The catalog declares
  it with the module; a module that is off has none, so its steps go with
  it.
- **Modules Homes only leads to.** The box's apps Homes has no page for
  are modules too, so their setup has a home and an off one leaves its
  trace (§10): `fotos` (immich), `passwoerter` (vaultwarden), `filme`
  (media). They are on while Laib runs their service, read once at start
  from `laib status` (Homes has no wiring of them; ADR §5's "derived from
  what the box runs"). Laib silent at start: off, saying it could not
  tell. `HOMES_MODULE_AUS` switches them off like any other.
- **One task per person** for each module that is on and has an app, and
  for the Homes app (`internal/einrichten`); done when Homes can tell (a
  paired phone; the phone's installed apps, `POST
  /napi/einrichtung/apps`; Jellyfin's devices with the key Homes has) or
  ticked by hand; kept in `HOMES_DATA/einrichtung.json`.
- **`GET /napi/einrichtung`** is the app's view of it (docs/REFERENCE.md);
  `/napi/kann` is unchanged but lists the three modules.
