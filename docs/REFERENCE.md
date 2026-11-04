# Homes in detail

The full reference of what Homes serves: every page, its states and words,
the `/napi` API the app speaks, and the areas under Mehr. The short version
is the [README](../README.md); how areas become modules is
[ADR 0001](adr/0001-modules.md). Issue numbers `haus#…` are this repo's
issues from before it was renamed (GitHub redirects them).

**Homes** is the family's surface for the box that [Laib](https://github.com/mdopp/laib)
runs: pages for people, cards, and `/napi`, the API the widgets of the Homes
app ([mdopp/homes-android](https://github.com/mdopp/homes-android)) speak.
Homes renders and decides nothing; Laib acts; the Bote is who you talk to,
and messages to people go through their chat channel, not the app (laib ADR
0014, 0016, 0022). The assistant has one name for the whole box, set once in
Laib (box.yml `assistant:`, reaching Homes as `HOMES_ASSISTANT`; *Homes*
without it). The
app's names (scheme `cloud.dopp.homes`, token prefix
`homes_device_`, the Box keys under `/napi/box`) live in `internal/app`; a
`/napi` path wants a paired device (`Authorization: Bearer homes_device_…`,
else 401, the unknown paths too); one Homes does not serve, or whose backend
is not set, then answers 404 `{"ok":false,"reason":"not_found"}`.


## Pages, cards and `/napi` (laib#28)

- `GET /start` — the family's page behind Authelia (haus#12, haus#35):
  *Zuhause* and the day, then one part per question a family member has.
  *Im Haus*: who is home (`person.*`: "Anna und Ben sind da"), the lamps
  that are on with their names and one button (*Aus* for one, *Alle aus*
  for several; a group, any entity with an `entity_id` list, is not a
  lamp), what plays on a `media_player` with *Pause* when it can pause.
  One thing is one row, on Start, Medien and Räume alike (haus#83): HA
  devices that share an identifier (dashes and case aside: a Chromecast
  through Cast and through Music Assistant) are twins, shown and steered
  through the one that plays, else the paused one, else not Music
  Assistant's mirror; a Jellyfin session is someone's app, on Medien only
  while it plays. A player refusing a command (HA 500) is said as such.
  *Deine Geräte*: the person's own things as the app's tiles (homes-android
  #245, variant A2; laib ADR 0017: favorites go to Homes):
  `HOMES_DATA/favoriten.json`, `{"<Remote-User>": ["light.buero", …]}`,
  read on each page load, in the person's order, at most twelve; what a
  thing's page would not show (not a card domain, unknown to HA, hidden
  by HA) is left out. The head says *Alle →* (to Geräte) and *Tippen
  schaltet, lange drücken öffnet das Gerät.* Each tile is the widgets'
  picture on a round of the state's colour (a lamp lit or dark, a blind
  up, part-way or down, the garage door up or down, the padlock), words
  only where the picture cannot say it (moving, not reachable, jammed, a
  heating's reading, *schaltet …* right after a tap). A tap switches a
  lamp or a switch, opens a blind's sheet, opens what is shut or shuts
  what is open on a cover without a position; a lock and anything else
  lead to their page; a long press opens the page (script). A dimmer's
  *60 %* or *aus* and a blind's *32 %* open the sheet. On a phone two
  columns: a lamp or a lock half the row, a cover across it with ▲ ■ ▼ (a
  garage ▲ ▼ without stop), a tile left without a neighbour across it;
  from two cells of 320 px on, every tile one cell as one line, in order.
  While the page cannot reach Homes (offline, the live stream or a fetch
  failing) the tiles grey under *Box nicht erreichbar … Stand 08:14*. None says how
  to get one (*Tippe unter Geräte bei einem Gerät auf ☆,
  dann erscheint es hier.*); no `Remote-User`, no store: no part.
  *Heute und demnächst*: appointments and open reminders of the next 14
  days together, by time (overdue first, one without a date last,
  *offen*), at most six, each leading to its page; none says so and how it
  fills. Notes are not on Start: they are under Mehr.
  The box is one calm line at the bottom (*Alles in Ordnung · eure Daten
  sind von gestern Abend gesichert*; without *Alles in Ordnung* when a part
  of the page does not answer); a service down or the data not backed up
  for three days moves it to the top, saying what it means (*Gerade nicht
  erreichbar: Fotos.*, *Eure Daten sind seit 3 Tagen nicht gesichert.*) and
  who is on it: amber when the box mends it itself, red only when a person
  is needed (nobody automatic on it, or the data not backed up). The admin's words (services, places, the last change,
  Rückgängig) are /box's. Start does not wait for laib's status (a
  second, haus#67): Homes keeps it 45 s and waits for a read at most
  250 ms; a page served before laib answered has the line's two places
  (*Wie es der Box geht, wird gerade nachgesehen …* at the bottom) and a
  script that fetches `GET /start/box?ruhig=0|1` (`{oben, unten}`, the
  same read, `ruhig` whether the rest of the page answered), or says
  laib's plain line when that fails.
  Each part only when its backend is set (`HOMES_HA_URL` + `HOMES_HA_TOKEN`,
  `HOMES_RADICALE_*`); one not set is not mentioned. One not
  answering within 4s is one plain line where its rows would be (*Licht
  und Geräte antworten gerade nicht. Schalten geht im Moment nicht.*, *Der
  Kalender antwortet gerade nicht.* once, a line in *Deine Geräte* when its
  file or HA does not), never its name or its error, which go to Homes'
  log; Laib not answering is *Wie es der Box geht, lässt sich gerade nicht
  sagen.* at the bottom. The page is always a 200. Cards are one renderer,
  `internal/cards` (title, rows, optional link; a list, boxes, tiles or a
  time column; a row may carry one button), for the pages and later the
  widgets' JSON. `/` redirects to `/start`; every other page path Homes
  does not serve is a 404 in the family's frame: *Diese Seite gibt es
  nicht.* and *Zur Startseite* (haus#12).
- The family's page frame (haus#35): the house palette (the chat's tokens
  and homes-android's `colors.xml`: ground `#070709`, cards `#14141A`,
  orange `#F97316` for what you can do, amber `#FFC107` for what is on,
  green and red only for a state, never alone), dark or light as the phone
  is, Geist served by Homes itself at `/schrift/…woff2` (OFL, vendored in
  `internal/pages/schrift`; no request to Google, the phone's own font
  when it does not load), Homes' icon under `/symbol/` (the terminal prompt
  inside a green house, the app's mark; `internal/pages/symbol/icon.svg`
  is the master, `tools/icons` renders the favicon, the touch icon and the
  web app manifest's icons from it), and one selection on every family page
  (haus#67; owner 2026-10-24: every page in exactly one tab, the page named
  like its tab): on the phone a bar at the bottom, icon and name — Start ·
  Geräte (`/raeume`: what plays on top as *Läuft gerade* with its buttons
  and ☆, once; then the rooms; `/medien` stays reachable and counts as
  Geräte) · Haushalt (Kalender, Einkauf, Notizen as pills on top of each;
  the tab leads to the first this box has) · Energie · Mehr — each only
  when its backend is set. `GET /mehr` holds Dokumente (owner 2026-10-27:
  Dokumente unter Mehr, haus#159), the music, Aufgaben, the
  Netzlaufwerk password, Box for admins, each only when this box has it,
  and *Konto* with who is signed in and *Abmelden*. Start: the day (*Heute und demnächst*) first, then *Meine
  Geräte*, *Im Haus*, and Energie in one line (the roof, the house, the
  battery's charge, the grid) leading to Energie. From 900 px wide the bar is a list on the
  right, Mehr open in it (its entries, who is signed in, Abmelden; Mehr's
  own entry is the phone's), the page and its entry marked, and the cards
  in two columns beside it. At the top of a page only the login, small
  (haus#61's Abmelden moved under Mehr). The assistant's name stays off the
  top of the family's pages; it appears where a sentence says how to
  address it („Sag Homes: Merk dir …“).
- Setup per module (owner 2026-11-04: „die Einrichtung gehört zur
  Funktion, als Aufgabe pro App“). Each module declares its setup in the
  catalog (`modul.Einrichtung`, `cmd/homes/module.go`): the address to
  open, its own Android app (name, package, Play link) or none, short
  steps (`internal/texte`, `schritt.*`) and the vendor's guide. A module
  that is on and has an app is one task per person
  (`internal/einrichten`), „<Titel> auf dem Handy einrichten“, and so is
  the Homes app; a module that is off has none. Today: Sprechen
  (Signal, with `HOMES_BOTE_SIGNAL`), and four modules Homes only leads
  to, on while Laib runs their service (asked once at start: `laib
  status`): `fotos` (immich, the Immich app), `passwoerter` (vaultwarden,
  Bitwarden), `filme` (media, Jellyfin), `hoerbuecher` (audiobookshelf,
  the Audiobookshelf app, „Hörbücher und Podcasts“; owner 2026-11-04 — on
  without Homes' ABS token too, and the only task for that app: Podcasts
  has none). Kalender has no setup task (owner
  2026-11-04: the iPhone's way is to be thought anew first). A task is done when Homes can
  tell — a phone paired for the person (the Homes app), the phone
  reporting the module's app installed (`POST /napi/einrichtung/apps`),
  Jellyfin knowing a device of theirs other than its web page (`GET
  /Devices` with `HOMES_JELLYFIN_KEY`) — or when the person ticks it on
  the module's block („Ist eingerichtet“, `POST /einrichten/<name>`, from
  Homes' own page, back to `zurueck`). Done tasks are kept per person in
  `HOMES_DATA/einrichtung.json` (`{"anna":{"fotos":{"am":"2026-11-04",
  "wie":"hand|app|erkannt"}}}`). The block sits on the module's own page
  (`/podcasts`, `/sprechen`) on top while the task is open,
  and shrinks to a quiet „Auf einem weiteren Gerät einrichten“ at the
  bottom once done; a module without a page has `/einrichten/<name>`.
  *Deine Aufgaben* (`/aufgaben`) lists them with Homes' other tasks
  (Netzlaufwerk-Passwort, Discogs) and Laib's for the person; the number
  open stands beside its heading, beside *Mehr* in the bar, beside
  *Aufgaben* in Mehr's list and in its row on `/mehr` — Homes' own, plus
  Laib's once a page has read Laib's list (never asked for the counter).
  `/willkommen`, where the first login lands: „Hallo <Vorname>“, the
  username, and *Deine nächsten Schritte*, the open tasks of Homes' own as
  links (or „Alles eingerichtet.“).
- `POST /schalten` — a family page's button (haus#35): `geraet`, `tun`
  (`light|switch.turn_on|turn_off`, `media_player.media_pause`, Geräte's
  `cover.open_cover|stop_cover|close_cover|set_cover_position` (haus#64), or
  `alle-lichter-aus`: every lamp *Im Haus* counts), a slider's `wert`
  (0–100, haus#64: `light.turn_on`'s `brightness_pct`, 0 is
  `light.turn_off`; `set_cover_position`'s `position`, which wants one; any
  other `tun` with a `wert` is 400) and `zurueck` (Start or
  a thing's page; anything else is Start). Each call passes the widgets'
  own rule (`internal/schalter`, below); nothing else in the form rides
  along. Who switched is said (haus#82, haus#89): each switch done — here
  and through `/napi/ha/call`, there the person who paired the device —
  is one log line `ha person=<Remote-User> service=<d.s> entity=<id>
  context=<HA's context id>` and the same text as a `laib_note` (kind
  `watch`), so HA's logbook (user „Homes“, the account Laib made for it) and Laib's journal together name
  the person. The note never holds up the switch: a failed one is a
  warning, Laib refusing `note` to the homes hand is logged once. Only with `Remote-User` and from Homes' own page
  (`Sec-Fetch-Site: same-origin`, or, from an older browser, an `Origin`
  of Homes' host; neither header is a 403 too), else 403; then 303 back. HA not answering is one plain
  line and the way back. No script: a plain form.
- `POST /raeume/<id>` `stern=an|aus` — the ☆ on a thing's own page pins it
  to the person's *Deine Geräte* or takes it off (`favoriten.json`, their
  own list only, at most 50, the file replaced whole, a file that does not
  read never written over); same guards as `/schalten`, then 303 back.
- `GET /raeume`, `/kalender`, `/notizen` — the family's other pages
  (haus#12), from the same cards, in the family's frame. A page whose
  backend is not set has no tab and, reached anyway, says in one sentence
  what is missing (an admin also reads what to set up), never a silent
  redirect to Start (laib#418); one
  not answering within 4s is Start's plain line where its rows would be,
  once, never its name or error, which go to the log; the page is a 200
  (haus#33). Anything but GET/HEAD is 405.
  - *Räume*: one card per Home Assistant area (the entity's own, else its
    device's), rooms by name: lights (on/off, brightness), heating (current
    and target temperature), covers (open/closed, position), media (what
    plays); groups left out, at most 40 rows a room; entities without an
    area in *Ohne Raum* at the end. What HA hides (`hidden_by` set, or
    `entity_category` config/diagnostic) is left out here and on Start.
    Temperatures carry HA's unit: the entity's `temperature_unit`, else
    `unit_system.temperature` from `/api/config`, °C only when HA says
    neither. The rooms, the hidden entities and the unit are read once and
    kept 5 minutes (the rooms shared with `/napi/portal/active`), refreshed
    in the background whoever asked; until the registries have been read,
    the page says it is reading them rather than put everything in Ohne
    Raum. HA not answering: *Licht und Geräte antworten gerade nicht.* Each
    thing links to its own page, where a lamp or a switch is switched and
    the ☆ pins it to Start; every row has its ☆ here too.
    A lamp HA can dim (a `brightness`, or a colour mode other than
    `onoff`) has a slider 0–100 % beside its switch (haus#64; a tap on the
    switch still switches); a cover with a `current_position` its slider,
    and Hoch, Stopp and Runter as its `supported_features` allow. Both are
    plain forms to `/schalten`, the slider sent when the finger lets go
    (one `onchange` attribute; without script, its *Setzen* button); a
    cover's sheet has Auf, ½, Zu and Stopp. Only a cover whose
    `device_class` is named and is no garage, door or gate gets the slider
    (`schalter.CoverFree`, the rule's own line); a garage, door or gate
    asks before Hoch (*Garagentor öffnen?*, a popover: its *Öffnen* posts
    `bestaetigt=ja`, the only way the page sends a confirmed call, and only
    for `cover.open_cover`), as the app does. A cover with no class says to
    set *Anzeigen als* in HA.
    A cover wired the wrong way round is listed in `HOMES_COVER_INVERTED`
    (cover entity ids, comma-separated): Homes shows it turned over (its
    position 100 − HA's, open where HA says closed) and sends Hoch as HA's
    `close_cover`, a position as 100 − the slider's, on every family page;
    the widgets' `/napi` stays as HA says. It is a property of the house's
    wiring, not of a person, so it lives in the template's config, not in
    `favoriten.json`.
    *Weiter in <App>* (haus#64), one pattern for every page that hands
    over to another app of the box (`pages.App`, `Weiter`): a link *In
    <App> öffnen* and a line of what is there, from that app's public
    address in the config; no address, no link. Geräte has *In Home
    Assistant öffnen* at the top (`HOMES_HA_PUBLIC_URL`, e.g.
    `https://home.<domain>`; only while HA answers), and a thing's page
    its device's page in HA (`/config/devices/device/<device_id>`, from the
    entity registry), else the entity's history (`/history?entity_id=…`).
  - *Kalender*: *Heute* (the day's appointments with time and place),
    *Nächste 14 Tage*, *Erinnerungen* (open reminders, overdue said so; one
    due on a day, not at a time, shows the day and is overdue the day after),
    every calendar of the Radicale account, at most 50 rows a card. Each
    reminder links to its own page. Radicale not answering: *Der Kalender
    antwortet gerade nicht.* once, where the appointments would be, or in
    *Erinnerungen* when only the reminders fail. The calendars from outside
    that laib's kalender mirrors (laib#495: each person's `google-*` and
    `abo-*`, read only) are in it, and so are the Bote's *Termine*; a
    calendar *nur für mich* (`<person>/privat-*`) only on that person's
    page. A row names its calendar unless it is the Haushalt, in the colour
    the person chose.
  - *Deine Kalender* (`/kalender/verwalten`, haus#66), linked from
    Kalender: every calendar with where it comes from (*aus Google, nur
    lesen · verbunden seit … · zuletzt abgeglichen vor 5 min*, laib's
    problem when there is one), a colour and *Ausblenden* (the person's own
    view, `ansicht.json` in Homes' data folder; Radicale and the phones are
    untouched), *Trennen* for a subscription, *Löschen* for a calendar
    Homes made, and *Adresse fürs Telefon* (the CalDAV address from
    `HOMES_RADICALE_PUBLIC_URL`, with the DAVx⁵/iPhone sentence; the same
    for the address book). *Kalender hinzufügen* has three ways: *Google-
    Kalender verbinden* (Google's *Geheime Adresse im iCal-Format*, handed
    to laib once as `laib_calendar`'s stdin and never shown again),
    *Öffentlichen Kalender abonnieren* (Feiertage or Schulferien of a
    Bundesland from openholidaysapi.org, or any ICS address), *Neuen
    Kalender anlegen* (`haus/familie-<name>` für die Familie,
    `<person>/privat-<name>` nur für mich). *In Radicale öffnen* is the
    page's *Weiter in <App>*. How often a connected calendar is mirrored
    (haus#96, laib#619): *Wie oft abgleichen?* when it is added (alle 15
    Minuten · täglich · wöchentlich · monatlich; Google suggests every run,
    a public calendar monthly), said in its row, and *Abstand ändern* there
    (`laib_calendar every`, the address untouched).
  - A note's head (haus#116): front matter `art` (notiz, rezept, album,
    reise, liste), `tags` (lower case, words joined by `-`) and `quelle`;
    every other line of an older head is kept. *Notizen* filters by tag
    (`?tag=…`, several combine, each a pill with its count), lists kind
    and tags at each note; *Neue Notiz* and *Bearbeiten* set them in fields
    (offered tags to tick, others typed), the note's page shows them with
    links. The assistant: `notiz_anlegen` takes `art` and `tags`, `notizen_mit_tag`
    lists by tag or says which tags there are, `notizen_suchen` names them.
  - *Aus Link merken* (haus#117, `internal/merken`): a link — on Notizen
    (`POST /notizen/merken`: `link`, `hinweis`) or sent to the assistant
    (`notiz_aus_link`) — becomes a note. Homes fetches the page itself, only
    public addresses (no home network by address or name, every redirect
    checked), 3 MB, 20 s. The page's JSON-LD makes a recipe (portions,
    time, ingredients, steps), an album (band, album, year, rating, the
    review), a trip (place, provider, price); else the house model's core
    and tags (`judge.Summarize`), else the page's description. Kind, tags
    (the hint's #words too) and `quelle` in the head; the link and the
    page's text at the end. A link kept once opens its note again; a page
    that answers but cannot be read keeps link and title, said so.
  - *Notizen* says where notes come from (the house's „Merk dir …“, Neue
    Notiz, a file in `notes` on the share) and lets a person write them
    (owner, 2026-10-24): *Neue Notiz* (`POST /notizen`: `titel`, `text`;
    `notes.Dir.Create`, never overwriting) and, on a note's page,
    *Bearbeiten* (`?bearbeiten=1`: the whole file in a field;
    `POST /notizen/<name>`: `text`, `stand` = the file's time as read).
    A note changed since is not overwritten (409): what is saved now and
    the person's own text are both on the page. Only from Homes' own page,
    who wrote what in the log; a note longer than a page shows is read-only
    here. Then a search field (`GET /notizen?q=…`, the memory library
    below: 20 hits, by meaning) and the ten newest notes. When the library
    answers but no note is close to the question, the page says *Keine
    Notiz passt wirklich zu deiner Frage.* and shows what the words find
    under *Notizen mit Wörtern aus deiner Frage* (nothing more when they
    find nothing). When the library is not set, does not answer, or is
    still indexing, the field searches
    by words (the notes search above, its caps) and says so in one line
    above the hits: *Die Suche nach Bedeutung geht gerade nicht; gesucht
    wird nur nach den Wörtern.* By words, a sentence needs only one of its
    meaningful words (common and short words left out), the notes with
    more of them first; nothing found says *Nichts gefunden. Versuch es
    mit einem anderen Wort, das in der Notiz stehen könnte.*, without an
    example word (every word of the query has just failed). A hit's lines
    are plain text: a table row as each value after its column's header
    (`Datum 31.12.2024 · Strom 41.230`; a column without one, the value
    alone), no header row, pipes, list dashes or heading hashes, here and
    in the Haushalt's answers. With the documents in the library (below)
    the field says *Notizen und Dokumente durchsuchen* and finds both: the
    notes under *Notizen zu „…“*, the documents under *Dokumente zu „…“*,
    each with its date, sender and the passage that matched, leading to
    its page `/dokumente/<id>` (only with Paperless's public address); a
    search only documents answer shows no empty notes card. Nothing close
    is then *Keine Notiz und kein Dokument passt wirklich zu deiner
    Frage.* Paperless not answering is one line, *Die Dokumente antworten
    gerade nicht.*, and the notes are found as ever. `/notizen/<file>` (the
    note's file name, e.g. `einkaufsliste.md`; a slug or title works too)
    shows the note as text: paragraphs and line breaks, no Markdown
    rendering, never markup from the file. Outside the folder, hidden or
    missing is a 404. A folder that cannot be read: *Die Notizen lassen
    sich gerade nicht lesen.*, for the list, a search and a note alike.
  - *Dokumente* (`/dokumente`, under Mehr; haus#68): the ways into
    Paperless, each only when its config is set. *Per E-Mail ablegen*, a
    `mailto:` to `HOMES_DOCUMENTS_MAIL` (one plain address, else no button)
    with the hint that the attachments are filed and the subject becomes
    the title (Paperless fetches that mailbox itself, laib#521). *Dokument
    ablegen*, a file or the phone's camera and an optional title, stored
    into `HOMES_UPLOAD_DIR` exactly as the app's `/napi/upload` (same
    types, caps, all-or-nothing staging; `napi.Uploads`), then back to the
    page with *Liegt in Paperless* or the reason in words. *In Paperless
    öffnen* to `HOMES_PAPERLESS_PUBLIC_URL` (*Weiter in*); with
    `HOMES_PAPERLESS_LAN_ONLY=true` the line says *Nur zu Hause im WLAN.*,
    and from outside the house (the client address Caddy forwards is not
    private) a sentence replaces the dead link. Last, one line on what
    happens: OCR, then found under Notizen and by the Bote. None set: the
    page says so. `/dokumente/` without an id leads here.
    *Scannen* (haus#76, laib#568) reads Laib's `laib_scan status` (the scan
    service's status file) and says its state in a word: *Bereit*, *Scannt*
    with the page, *Nach Blatt n abgebrochen* with where the rest goes in,
    *Trennt die Dokumente*, *Legt die Dokumente ab*, *Fertig*, or the
    service's own sentence after an error; a stack running or waiting puts
    the section first, and while it moves on by itself the page reloads
    every 5 s. *Stapel scannen* only with a SANE scanner (`scanner: true`)
    and nothing running; after a jam *Weiter*, *Abschließen*, *Verwerfen*.
    Each button is `POST /scannen`, one `laib_scan` order for the person who
    pressed it (#35's rule), then back. Below, the inbox folder for a
    scanner that scans to the network drive
    (`\\<HOMES_SAMBA_HOST>\data\scan-eingang`) and the last five stacks,
    each one folded line (haus#197: when, who, „nur für“ whom, pages, how
    many documents or what came of it; the newest open, what a viewer opens
    or closes kept in their browser). Opened, its documents as Paperless has
    them now by the custom field `Stapel` = the stack id (Dokumente writes
    it, mdopp/dokumente#15; laib#909 makes the field): each a link to the
    document, and *Den ganzen Stapel in Paperless öffnen* as Paperless's own
    list filtered to it; a document deleted there is gone, a split one shows
    its pieces. A stack Paperless lists nothing for by the field (filing,
    failed, filed before the field) keeps Dokumente's own list, by id where
    Dokumente knows it, less what Paperless no longer holds by file name;
    one with nothing left says *Nichts mehr in Paperless*. A `start` or
    `weiter` the scan refused (its status `auftrag.angenommen: false`,
    mdopp/dokumente#16) shows the scan's reason where *Startet …* was, at
    once, the button back. *Trennblätter* (haus#161): one line on what they do,
    a number field (5, 1–50) and *Trennblätter drucken*, which opens
    `GET /scannen/trennblatt?anzahl=n` — that many A4 sheets, one per page
    (`@page` A4, a page break after each), each with a Code 128 barcode
    reading exactly `PATCH T` (code set B with its checksum, drawn by
    `internal/code128` as inline SVG: 1.2 mm a module, 13.4 cm plus a 12 mm
    quiet zone each side, 3 cm tall), *PATCH T* under it and *Trennblatt –
    zwischen zwei Dokumente legen. Wiederverwendbar.*; *Drucken* and
    *Zurück* above them, not printed; nothing loaded from elsewhere. Laib's
    scan service (laib#814) and Paperless-ngx split a stack at that sheet.
    A number outside 1–50 is the nearest end, and more than 50 says so.
    A box without the service shows the family no section;
    a Laib before v0.20.0, or a policy.yml without `scan` for the homes hand,
    tells an admin so.
    *Scanner einrichten* (haus#77, laib#571), admins only, folded unless no
    scanner is set, a search runs or an order just came back: the last
    check (`pruefung`: *Scanner antwortet*, or the service's reason) with
    *Erneut prüfen* and *Probeseite* (orders `pruefen`, `probe`); *Scanner
    suchen* (`suchen`), and while it runs the page reloads, then each find
    of `suche.funde` with *Diesen einrichten*; *Adresse von Hand*
    (`epson2:net:<ip>`, refused unless laib#571 would take it); and for a
    scanner that scans to a folder what goes into its own menu (the share
    `\\<HOMES_SAMBA_HOST>\data`, the folder, a Netzlaufwerk account).
    A setup order is followed where it was pressed (haus#80): the button
    says *Sucht …* at once, the redirect carries `?tut=<order>&seit=<unix>`,
    and the page looks again every 2 s until the result the order writes
    (`suche.at`, `pruefung.at`) is as new as the order — then shows it at
    the button (nothing found: the sentence and the field by hand, focused;
    Laib's `einzelheiten`, the raw error, folded under *Einzelheiten*, haus#88) — or says that no result
    came within 60 s (search), 30 s (check), 2 min (probe).
    Einrichten is `laib_scan einrichten [--geraet]`: one proposal, the
    service with that scanner on a box without it (*Scannen einrichten*),
    its `SCANNER` otherwise; back with a fixed sentence (no device in the
    URL) and the way to the inbox with the proposal's number. Homes decides
    nothing: Laib's dial and the owner do.
  - The item pages (haus#28), what a widget opens on a tap: `/raeume/<entity_id>`
    (a thing: its state in words, since when, its room; only an entity of
    a card domain, `cards.Domains`, that HA does not hide — a person, a
    tracker, a hidden entity are a 404),
    `/kalender/erinnerung/<uid>` (a reminder: open and due, done, or
    cancelled), `/kontakte/<uid>` (a contact: numbers and email addresses to
    tap, the birthday, the postal addresses), `/dokumente/<id>` (a 302 to the document's page in
    Paperless, `HOMES_PAPERLESS_PUBLIC_URL`; without it a 404), and a note's
    `/notizen/<file>`.
    Something not there is a 404 that says so, with the way back. A
    backend that does not answer is a 200 with that page's plain line and
    the way back, never "gibt es nicht" (haus#33): HA's line, or Räume's
    while HA's registry is not read yet; the calendar's; *Die Kontakte
    lassen sich gerade nicht lesen.*; the notes'. A document's page reads
    nothing from Paperless, so Paperless's own page answers for it. Their
    paths are `internal/cards/pages.go`, one place for the routes and the
    rows.
- `GET /box` — the admins' page behind Authelia, linked from Start for
  the `admins` group. Homes itself sends anyone whose `Remote-Groups` lacks
  `admins` from `/box` and everything under it (Geräte included) to
  `/start` with 303, since Authelia here only restricts per host. That
  says who sees the admin page; it is not a verdict on anything, and
  `/napi` and `/pair-device` are untouched. The page is the family view of the box and
  does not repeat Laib's own page (haus#157, owner 2026-10-27): the three
  guarantees, readiness, the Inbox as a count (*3 Karten warten auf eine
  Entscheidung.*) with *Entscheiden auf der Laib-Seite* (`<laib>/inbox`,
  the one approval surface, laib ADR 0012), the newest morning report
  with a link to Laib's journal (`<laib>/journal`; no journal copy), the
  Werkstatt's tickets when set. *Dienste* (`/box/dienste`) lists only
  what is not green, red first, by Laib's names (haus#156), or *Alle
  Dienste laufen.*, and links *Alle Dienste auf der Laib-Seite*
  (`<laib>/`). It comes at once
  (haus#81): the frame and each part's place with „lädt …“; its script
  fetches every part at the same time from `GET /box/teil?was=<part>`
  (`stand`, `inbox`, `einladen`, `bericht`, `aufgaben`, `genutzt`, `werkstatt`: JSON,
  slot id → HTML), each within 10 s, a failed one said in its place.
  `?ganz=1`, where `<noscript>` sends, reads all parts at the same time
  and comes whole. *Person einladen* (`POST /box/einladen`, haus#56) takes
  a Signal number too (haus#87, laib#596), offered only once Laib's
  `person_invite` lists `signal` in its `tools/list` schema (kept 5 min):
  typed freely, sent as E.164 (`+49…`), a wrong one refused; after it,
  that the number waits for the owner's approval, with the proposal.
  *Signal-Nummer nachtragen* on the same card (`tun=signal`: `benutzer`,
  `signal`), once Laib has `person_signal`: one proposal for someone who
  already has an account, said the same way.
  Every page request must
  carry `X-Homes-Proxy: <HOMES_PROXY_SECRET>`, which only Caddy adds; the
  loopback port is shared with every host-network container, so without
  the secret nobody is anybody.
- `/napi/box/{home,services,approvals,upgrades}` — the four Box keys the
  widgets read, from `laib status`, `laib inbox`. `pendingUpdates` and
  `upgrades` are the open proposals of kind `template-update` (`laib
  template sync --propose` files one per update); `pendingApprovals` and
  `approvals` are the others. An update is counted once, as an update. `POST …/services/{name}/operate`
  (restart). `POST …/approvals/{id}/approve|deny` answers 403: Homes decides
  nothing (haus#1); approvals reach the admin through the relay (laib ADR 0012).

- Pairing: in the app, enter `https://homes.<domain>`. The app opens
  `/pair-device` in a Custom Tab, Authelia logs the person in, they confirm,
  and Homes redirects to `cloud.dopp.homes://pair#token=homes_device_…&id=…`,
  the token in the fragment (haus#6). `GET /box/geraete` lists the devices,
  revocable, and can mint a token whose setup URI
  (`cloud.dopp.homes://setup?server=…&token=…`) goes into a QR code the app
  scans. That URI is QR content, not a link: the app only opens `…://pair`.
  Tokens live hashed in `HOMES_DATA/devices.json`; `/napi/whoami` answers for
  them. Only a bearer with `homes_device_` is a device — and, until every
  phone runs the Homes app (laib#1021), one with the old Haus app's
  `haus_device_` whose hash is stored: a phone paired with Haus keeps
  working. `GET /napi/app` (a device token) answers
  `{"name":"Homes","paket":"cloud.dopp.homes","quelle":"https://github.com/mdopp/homes-android"}`:
  the old app (`cloud.dopp.haus`) sees the package is not its own and tells
  the person to install Homes and remove Haus afterwards.

- The old address: the box routes `haus.<domain>` here too (laib#1021). A
  request there for anything but `/napi/…` — the only paths the old app
  calls with its device token — is redirected (308) to the same path on
  `HOMES_PUBLIC_URL`; `/napi/…` is served there as it is (OkHttp drops
  `Authorization` on a redirect to another host). Requests from inside the
  box (`host.containers.internal`, `127.0.0.1`) are never redirected.

- The person's modules and setup tasks, for the app (owner 2026-11-04;
  the app's launcher and its setup list): `GET /napi/einrichtung` with a
  device token → the device's person's modules that are on (not the
  admins' ones), each with an address to open or its own app:

  ```json
  {
    "version": 1,
    "offen": 1,
    "module": [
      {"name": "fotos", "titel": "Fotos", "icon": "fotos", "web": "https://photos.example.org",
       "app": {"name": "Immich", "paket": "app.alextran.immich",
               "store": "https://play.google.com/store/apps/details?id=app.alextran.immich"},
       "nur_web": false,
       "aufgabe": {"titel": "Fotos auf dem Handy einrichten", "erledigt": false,
                   "einrichten": "https://homes.example.org/einrichten/fotos"}},
      {"name": "filme", "titel": "Filme und Musik", "icon": "filme", "web": "https://media.example.org",
       "app": {"name": "Jellyfin", "paket": "org.jellyfin.mobile",
               "store": "https://play.google.com/store/apps/details?id=org.jellyfin.mobile"},
       "nur_web": false,
       "aufgabe": {"titel": "Filme und Musik auf dem Handy einrichten", "erledigt": true,
                   "am": "2026-11-04", "wie": "app", "einrichten": "https://homes.example.org/einrichten/filme"}},
      {"name": "einkauf", "titel": "Einkauf", "icon": "einkauf", "web": "https://homes.example.org/einkauf", "nur_web": true}
    ],
    "ziele": {
      "einkauf": "https://homes.example.org/einkauf",
      "zeitleiste": "https://reitti.example.org",
      "scannen": "https://homes.example.org/napi/upload"
    }
  }
  ```

  `name` is the module's key (as in `/napi/kann`), `icon` the app's key
  for its picture (the module's name; an unknown one gets the app's
  default). `web` is the module's page in Homes where it has one, else its
  service's own address; absent when there is none. `app` is absent and
  `nur_web` true for a module without its own app. `aufgabe` is the
  person's task, only for a module with an app: `erledigt`, since `am`,
  and `wie` (`app`: the phone said it is installed, `hand`: ticked on the
  page, `erkannt`: the service knows a device of theirs); `einrichten` is
  the page with the module's steps, for the app to open. `offen` is how
  many of these tasks are open (the web's counter adds Homes' other tasks
  — the network drive's password, Discogs — and Laib's for the person).
  `ziele` are the launcher's shortcuts this box has, each only when its
  module is on: `einkauf` the shopping list's page, `zeitleiste` Reitti
  (`HOMES_REITTI_PUBLIC_URL`), `scannen` the camera capture into Paperless
  (`POST /napi/upload`, the same path the share target uses). Fields are
  only ever added; a change that cannot be added is `version: 2`.
  `POST /napi/einrichtung/apps` `{"pakete": ["app.alextran.immich", …]}`
  (at most 64 KB; the Android package ids the phone has, from
  `PackageManager`, for the packages the answer names) ticks each open task
  whose app is among them, `wie: app`, and answers the same as the GET; a
  task is never un-ticked by a package that is missing later. A body that
  is no such JSON is 400 `{"ok":false,"reason":"bad_request"}`; a tick that
  cannot be kept 500 with a German `error`. Both want a paired device
  (401), and are 404 `not_found` on a Homes without them (an older one):
  the app then behaves as before. `GET /napi/kann` (ADR 0001 §2) is
  unchanged; it lists the three new modules like any other.

- Teilen → Homes → Musik (owner 2026-11-04: songs and ZIPs come from the
  phone by sharing, not by a picker on the page): `POST /napi/musik`,
  multipart, one or more `file` parts (other parts are skipped), each a
  music file (`.mp3`, `.flac`, `.m4a`, `.ogg`, `.opus`, `.wav`) or a
  `.zip`, by its file name's extension, up to 4 GB a file; the request may
  take an hour. Each file becomes one upload at the top of beets' inbox
  (`HOMES_MUSIC_INBOX`), under the phone's name for it without any
  folder, beside one of the same name (`Lied (2).mp3`), never over it:
  written as `._<name>.<n>.part` (the importer skips dot files), the
  device's person's sidecar `._<name>.von` beside it (laib
  templates/beets, "Uploading from Homes": the importer's message goes to
  them), then renamed into place. An album goes as a ZIP: single files are
  sorted in as single songs. `{"ok": true, "dateien": [the names they
  got]}`; a refusal is `{"ok": false, "error": a German sentence,
  "dateien": what arrived before it}` — 400 for what is not music, an
  empty file or no file, 413 over 4 GB, 507 the disk full, 500 a disk
  error. What the importer made of it is the Musik page's (below). It
  wants a paired device (401) and is 404 `not_found` without an inbox;
  `GET /napi/kann` names `musik` under the module's `teilen` then.

- The assistant's name: `GET /napi/assistent` with a device token →
  `{"name":"Homes"}` — `HOMES_ASSISTANT` as Laib sets it from box.yml
  `assistant:`, *Homes* when unset or empty. That is all the app takes
  from the box about the assistant: its colours are fixed in the app, its
  icon is the app's launcher icon.

- The house paths, from Home Assistant (haus#10, set by `HOMES_HA_URL` +
  `HOMES_HA_TOKEN`, a long-lived token; unset, they are 404):
  `POST /napi/ha/call` (`{entity_id, service, data?, confirmed?}`; only
  `light|switch|cover|climate|lock`, and `media_player.media_pause` (haus#35;
  any other player service is 400 `unsupported_service`), else 400; the
  rule is `internal/schalter`, the family's pages' too; the service's domain is the
  entity's; `data` cannot name another target; any `lock`, and on a
  garage/door/gate cover — or one whose `device_class` cannot be read —
  anything but `close_cover`, `stop_cover`, `close_cover_tilt`,
  `stop_cover_tilt`, answers **403 `sensitive_action`** without
  `confirmed:true`; HA refusing the call is 400, HA in trouble is 502
  `ha_unavailable`, HA's detail stays in Homes' log) and
  `GET /napi/portal/{state,energy,entity-history,active}`. Every one wants
  a paired device. The shapes are the ones the app's `data/ApiClient.kt` parses; no JSON `null` goes out
  (Android reads it as `"null"`). Every card names its thing's page
  (`page`, `/raeume/<entity_id>`).
- *Energie* (`GET /energie`, under Mehr): *Jetzt* as a flow picture
  (Solar, Netz, Akku, Haus as nodes; the ways the power takes, as thick
  as they carry, the sun into the house first; Autarkie and
  Eigenverbrauchsquote under it; a leg HA cannot reach drawn muted, never
  a zero), *Heute* (each counter since midnight, while the history does
  not show today), then the history
  (haus#65) from HA's long-term statistics (WebSocket
  `recorder/statistics_during_period`, `types: [change]`, in kWh):
  `?zeitraum=tag|woche|monat|jahr` — today by the hour, the last seven
  days, this month by day, this year by month — with the span's sums
  as the same flow picture (Solar, Eigenverbrauch = Solar − Einspeisung,
  Bezug, Einspeisung) and a
  bar chart Homes draws itself (SVG, no script, no library; Eigenverbrauch
  and Bezug above the line, Einspeisung below, each bar's numbers on
  hover, a legend, the same as a table; three series colours of its own,
  checked for colour blindness, dark and light). Then the comparisons,
  whole days only (today is not over): this month against the same days
  of the last and of the same month last year, the year so far against
  last year's, and the forecast to the year's end, marked *geschätzt*:
  what came so far plus the rest as it came last year, while HA does not
  have last year's days the daily mean so far, drawn in the history's
  own chart: in Jahr the months to come (Monat: the days) hatched in the
  same stacked shape, a heute line, last year as dashed lines (use above
  the zero line, Einspeisung below, gaps where HA lacks the month); the
  year-end figures right under the Jahr chart. The span's flow picture
  stays "bis heute". Each comparison is a headline number with a delta (arrow,
  sign, word) coloured by what it means for the household: more Solar and
  Eigenverbrauch good, more Bezug bad, Einspeisung neutral. A comparison whose earlier
  days HA does not have says *Vergleich ab <Monat Jahr>*, never a zero;
  one with part of them compares the days both have and says from which
  day. The statistic ids are `HOMES_ENERGY_STATS`
  (`bezug=sensor.…,einspeisung=sensor.…,solar=sensor.…`, any subset), else
  the counters below (their fallback names or `HOMES_ENERGY_TOTALS`'
  labels with *Bezug*, *Einspeis…*, *PV* or *Solar*). HA's statistics not
  answering: *Der Verlauf lässt sich gerade nicht lesen.*, Jetzt and Heute
  stay.
- The energy picture reads the entities `HOMES_ENERGY_FLOW`
  (`pv=sensor.…,house=sensor.…,grid=sensor.…,battery=sensor.…`, any subset)
  and `HOMES_ENERGY_TOTALS` (`<Label>=sensor.…,…`, in that order) name;
  every other power sensor is a circuit. Unset, it falls back to matching
  one house's German friendly names (`Aktuell erzeugter PV-Strom`, …).
  Each total carries `today`, the kWh it added since the house's midnight
  (`TZ`'s; the Energie page's Heute rule, `energie.Added`), for the
  app's one energy widget; a history that fails or has nothing since
  midnight leaves it out.
- The realtime pair `POST /napi/portal/watch` + `GET /napi/portal/events`.
  watch is Home Assistant's (`geraete`); the stream is the core's (ADR 0001
  H6): it answers on every box, with the house's frames while `geraete` is
  on and the account's `gemeinsam` frames while `konto` is. The stream is SSE and carries state changes only
  (laib ADR 0022 §2): `card_state`, a frame for each entity the device
  watches, the current cards again after every (re)subscribe, a sensor at
  most every 10s; and `active_set` (`{entity_ids}`, haus#28) for a device
  whose watch says `active: true`: at once to each of its streams when it
  opens or the watch turns active (once the set is known), then when the
  set of things that are on or open changes, whichever entity it was, at
  most every 10s, the latest set always, and only to a stream that has
  another. `: ping` every 90s, at
  most three streams per device; watch answers 503 while HA does not. The
  app holds it only while the screen is on.
- The camera widget, from Home Assistant too (haus#10):
  `GET /napi/portal/camera/{entity_id}/snapshot` passes HA's
  `camera_proxy` frame for a `camera.*` entity only (anything else 400,
  an unknown camera 404), and only a JPEG, PNG, GIF or WebP of at most
  8 MB (else 502), `Cache-Control: private, max-age=5`.
  `GET /napi/portal/start/addable` is what both pickers read: the device
  picker the actuators (`light|switch|cover|climate|media_player|lock`),
  the camera picker the cameras; `{ok, rooms:[{room, cards:[{entity_id,
  name, domain, state, device_class?}]}], automations:[]}`, rooms by
  name, the room-less last.
- The app's start screen (owner, 2026-11-02): `GET /napi/portal/favorites`
  is `{ok, favorites:[card]}`, the device's person's ☆ things (the same
  `favoriten.json` the pages pin to), in their order; a pinned thing HA
  lost, or one Homes draws no card for, is left out, none is `[]`, and a
  file that does not read is a 500 `favorites_unreadable`.
- The tool widgets, from the Haushalt source's backends (haus#10, the
  same variables as below; none set, they are 404): `GET /napi/defs/tool`
  lists a tool per backend that is set: `task` (*Erinnerungen*,
  `HOMES_RADICALE_*`), `contacts`, `doc` (`HOMES_PAPERLESS_*`), `note`
  (`HOMES_NOTES_DIR`). Each def names its
  row path (`/napi/portal/tasks|contacts|documents|notes`: the open
  reminders, soonest first, due in house time, each with `status` and
  its UID trimmed as `id`, and with `?done=1` the done (`done`) and
  cancelled (`dismissed`) ones after them, the latest first; contacts by
  name with the first number and address; the newest Paperless
  documents; the newest notes; at most 40 rows, no JSON `null`, a
  backend in trouble 502). Each row with an id names its item page
  (`page`, above); a document only when Paperless's public address is
  set. No def names a create page: Homes has none. A path whose backend is
  not set is 404.
  `POST /napi/action-callback` (at most 64 KB, else 413 `too_large`)
  knows one action; any other id is 404 `unknown_action`.
  `task.set_status` with `{entity_id: <the reminder's UID>, status:
  "done"}` ticks the reminder off in Radicale (`haushalt.ReminderDone`:
  found by its UID, trimmed, in every calendar, written back as it was
  plus `STATUS:COMPLETED`, `COMPLETED`, `PERCENT-COMPLETE:100`, only if
  its ETag is still the one read). Answers: done already 200; a series
  (`RRULE`, `RDATE`, an override) 409 `recurring`, since ticking off its
  master ends it (the phone does that); cancelled 409 `cancelled`; a
  phone's edit in between 409 `changed`; unknown **410** `not_found`
  (not 404, which the app reads as an unknown action); bad params 400
  `bad_params`; Radicale in trouble 502. It is not destructive (the
  phone unticks it), so it asks no `confirmed`; Homes adds, files or
  changes nothing else from a row.
- Share → Homes (haus#120): `POST /napi/merken` with JSON `{"text"}`,
  what the phone shared (a browser's link, often with the page's title
  around it). The first http(s) address is the link, the words around it
  the hint (`#tags` become tags); Homes keeps the page as a note the way
  *Aus Link merken* does. Answer `{"titel", "art", "kurz", "schon",
  "teilweise", "notiz"}` (`notiz`: the note's page on Homes, or "" without
  `HOMES_PUBLIC_URL`); a text without a link or an address at home 400, a
  page that does not answer 502, no notes 404 — each `{"error": "<a
  German sentence>"}`. Up to 75 s (a page, then the house model).
- The camera capture: `POST /napi/upload` (multipart, `file` parts, an
  optional `filename`) stores into `HOMES_UPLOAD_DIR`, the share's folder
  Paperless consumes; unset, 404. JPEG and PDF as the contract
  has them, and PNG, GIF and WebP beyond it (Paperless takes them), each
  only when its first bytes say so (else 415); 25 MB a file (413), 64 MB
  and 20 files a request. The name is the phone's, made plain (no
  folder, no hidden file, letters, digits, umlauts (decomposed ones
  composed), ` ._-`, anything else `_`), plus `-<8 hex>` and the type's
  extension. A file is written aside as `._haus-….part` (Paperless
  ignores `._*`; stale ones are swept at start) and renamed into place
  with `renameat2(RENAME_NOREPLACE)`, which Paperless's inotify consumer
  sees as `MOVED_TO`; without it (kernel, file system) a hard link. Either
  way it appears whole and never replaces another; a request is stored
  all or nothing. `{ok, id}` for one file, `{ok, files:[{ok, id}]}` for
  several, with no `url` (Homes serves no file back). Limits: 507 when the
  share has less than 128 MB free or runs full, 429 while two uploads
  run, 408 when a body stalls past two minutes; a disk error is 500
  `store_failed`, not the phone's fault.
- The phone's message backups (haus#207, laib ADR 0029; the app's side is
  homes-android#194): WhatsApp, Signal, SMS/call log and Discord files into
  `HOMES_ARCHIV_DIR/<person>/<quelle>/<pfad>`, where that person's archive
  imports them. The person is the device's (the uid it was paired with),
  never the request's. `GET /napi/sicherung/stand` → `{quellen:{whatsapp|
  signal|sms|discord:{zuletzt, dateien, bytes, wartend}}, frei_bytes}`;
  `POST /napi/sicherung/fehlend {quelle, dateien:[{pfad, groesse}]}` (≤ 5000)
  → `{fehlend:[pfad]}`, against Homes' ledger
  (`HOMES_DATA/sicherung/<person>/<quelle>.jsonl`: the size each pfad last
  arrived with; the importer deletes what it took, so the folder cannot
  say). The upload resumes: `POST /napi/sicherung/uploads {quelle, pfad,
  groesse}` → 201 `{id, offset}` (the same file again is the same upload,
  where it is), `HEAD …/uploads/{id}` → 204 `Upload-Offset`, `PATCH
  …/uploads/{id}` with `Upload-Offset` and `application/offset+octet-stream`,
  ≤ 16 MB → `{offset, fertig}`. A piece lands in `<person>/.teil/<id>`,
  synced, and is kept when the connection breaks mid-piece; the last one
  renames the file into place (replacing one of that name: WhatsApp's
  `msgstore.db.crypt15` keeps its name) and notes it in the ledger. pfad:
  relative, no `..`, nothing empty or hidden, ≤ 512 bytes, through
  `os.Root`; ≤ 20 GB a file. Refusals are `{fehler: <a German sentence>}`:
  400 a bad name or more bytes than declared, 404 an id that is not this
  person's, 409 `{fehler, offset}` a piece at the wrong offset (or while
  another holds the file past 5 s; a phone back from a dropped connection
  stops its own piece still hanging on the box and goes on), 413 a piece over 16 MB, 429 while four pieces arrive,
  507 when the disk lacks the rest of the file plus 1 GB, 503 without
  `HOMES_ARCHIV_DIR` (*Das Archiv ist auf dieser Box nicht
  eingerichtet.*). A piece may stall 45 s; parts untouched for seven days
  go (at start and daily). The page *Daten sichern* (`/daten-sichern`;
  the old `/nachrichten-sichern` redirects there; under Mehr when the box
  has the inbox) shows the same state for the signed-in person and, per
  source, what to do and what then happens: Signal, WhatsApp, SMS and
  calls, contacts, mail, calendars (owner, 2026-11-03; Discord dropped, the
  location history only pointed to). The sources are `whatsapp`, `signal`
  and `sms`. Its card *Für ein neues Handy* (haus#209) shows what the
  archive keeps in `letzte/` (the same as `/napi/sicherung/letzte`), with
  date and size, and the phone change in three steps.
  The source `sms` is the phone's SMS and call log
  (`sms-<zeit>.xml.age`, `anrufe-<zeit>.xml.age`), which the app encrypts
  on the phone to the person's archive before sending; Homes keeps them as
  the bytes they are. The key: `GET /napi/sicherung/schluessel` → 200
  `{empfaenger: "age1…"}`, read (through `os.Root`) from
  `HOMES_ARCHIV_DIR/<person>/.briefkasten.pub`, which the archive writes
  (one age X25519 recipient); 404 `{fehler}` until it did (*Dein
  Archiv ist noch nicht eingerichtet. Richte es einmal auf der
  Archiv-Seite ein, …*), 500 when the file holds anything else.
- A new phone gets the newest backup back (haus#209, laib ADR 0029
  amendment 2026-11-01): the person's archive keeps it in
  `HOMES_ARCHIV_DIR/<person>/letzte/` — `signal/signal-*.backup` and
  `whatsapp/Databases/msgstore.db.crypt15`, encrypted by the messenger,
  and `whatsapp/Media/<pfad>.age`, encrypted to the archive.
  `GET /napi/sicherung/letzte` → `{quellen:{signal|whatsapp:[{pfad,
  groesse, geaendert}]}}` (epoch ms; a medium's `pfad` without `.age`, its
  `groesse` the plaintext's, read from the age header). `GET|HEAD
  /napi/sicherung/letzte/<quelle>/<pfad>` gives the file with `Range` and a
  strong `ETag` (send it as `If-Range` when resuming: a backup replaced in
  the meantime comes whole, never mixed). The person is the device's, so
  another person's device gets 404. Media (`whatsapp/Media/…`) come only
  from the person's open archive: Homes asks its
  `/intern/whatsapp/medien/<pfad>` with `X-Archiv-Homes` and passes Range
  and the answer on; a closed vault is 423 (*Öffne dein Archiv, dann
  kommen auch die Fotos und Sprachnachrichten zurück.*), an archive Homes
  has no way into 503. The ways in: `HOMES_ARCHIV_INTERN="<person>=<url>
  …"` (each archive as Homes' pod reaches it) and
  `HOMES_ARCHIV_SECRET_<PERSON>` (laib's per-person suffix: upper case,
  `-` as `_`), the instance's `ARCHIV_HOMES_SECRET`.
- The phone's contacts (laib ADR 0029; optional in the app, behind
  Android's contacts permission): `POST /napi/kontakte`, `text/vcard`,
  all of the phone's contacts as concatenated vCards (≤ 64 MB, else 413),
  each with a `UID` (the app's Android lookup key; a card without is 400,
  naming it). Homes mirrors them, as its own Radicale account
  (`HOMES_RADICALE_USER`, which may read and write `<person>/handy-kontakte`
  and nothing else of the person's), into the device's person's address
  book `/<person>/handy-kontakte/` ("Handy-Kontakte", made when missing):
  each card at `<sha256(UID)[:32 hex]>.vcf`, written only when its text
  changed (Homes notes a hash of the phone's text on the card as
  `X-HAUS-INHALT`), and every card in the book the upload lacks deleted —
  the book is a mirror; an edit there is overwritten by the next upload.
  → 200 `{kontakte, neu, geaendert, entfernt}`. Refusals `{fehler}`: 403 a
  device of nobody, 409 an upload with no cards (a permission taken back
  never empties the mirror), an upload of that person already running, or
  the book changed under Homes (If-Match), 415 not `text/vcard`, 502 when
  Radicale refuses, 503 without Radicale (`HOMES_RADICALE_*`).
- The phone's positions, passed on to Reitti (owner 2026-11-02; optional
  in the app, its own switch and background-location permission): the app
  records where its person was and sends the points in batches to `POST
  /napi/standort {punkte:[{zeit (RFC 3339), breite, laenge, genauigkeit
  (m), hoehe?, tempo? (m/s)}]}` (≤ 1000 points, ≤ 2 MB). Homes passes them
  on, inside the box, to Reitti's Overland ingest (`POST
  <HOMES_REITTI_URL>/api/v1/ingest/overland`, GeoJSON points) with the
  device's person's own Reitti token (`Authorization: Bearer`), and
  answers 200 `{angenommen, verworfen}` only once Reitti said
  `{"result":"ok"}`; a point without a readable time, a place on earth or
  an accuracy is left out and counted (`verworfen`). The app keeps a batch
  on every other answer and sends it again: 409 `{fehler, grund:
  "kein_schluessel"}` before the person connected Reitti, 409 `grund:
  "schluessel_abgelehnt"` when Reitti refuses the token (401/403), 503
  `grund: "reitti_weg"` with `Retry-After: 300` when Reitti does not take
  them (no answer, an error, a redirect, a 2xx without its `ok`), 503
  `grund: "nicht_eingerichtet"` without `HOMES_REITTI_URL`. `GET
  /napi/standort` → `{verbunden, seit?}`, never the token. The token: the
  person pastes it once on the page *Standortverlauf* (`/standort`, under
  Mehr when the box has Reitti); Homes checks it with an empty batch
  (Reitti says yes only to a token it knows with a device attached), keeps
  it in `HOMES_DATA/reitti-konten.json` (0600) for that person alone, never
  logs or shows it. With it Homes keeps the newest point's time of each
  batch Reitti took (`zuletzt`, never past now; an imported history does
  not count). Once connected the page leads with *Zeitleiste öffnen* (into
  Reitti), then one line of state (*Verbunden · zuletzt aufgezeichnet
  heute 17:40*, or *Verbunden seit …* with the step in the app while no
  point has come), and ends with a quiet, asked *Trennen*.
- An old location history, shared to the app (owner 2026-11-03,
  *In den Standortverlauf übernehmen*): Google Maps' on-device export
  (`Timeline.json`, Android `{semanticSegments}` and the iPhone's array),
  Google Takeout's `Records.json` (`{locations}`), GPX (`<trkpt>`) and
  GeoJSON (a FeatureCollection of Points with a time). Reitti reads the
  Google formats only on its logged-in settings page (`/settings/import/…`,
  session and CSRF; its token filter covers `/api/` only), so Homes reads
  each file itself, point by point as Reitti's own importers do (v5.9.0:
  the same points — a visit is its place at start and end, a path its
  points; the same default accuracies; `rawSignals` and activities are not
  points there either) and hands them in batches of 25,000 to Reitti's
  token import `POST <HOMES_REITTI_URL>/api/v1/geojson/import` (multipart
  `file`, a FeatureCollection) with the person's token. Reitti files them
  into the timeline after its grace time (5 min); a point it has (same
  device, same second) is kept once, so an import sent again adds nothing
  twice. The file comes like a backup: `POST /napi/standort/import {name,
  groesse}` → 201 `{id, offset}` (the same name and size again resume; 409
  `{fehler, grund: "kein_schluessel"}` before the person connected Reitti;
  413 over 5 GB; 507 without room), `HEAD /napi/standort/import/{id}` →
  `Upload-Offset`, `PATCH /napi/standort/import/{id}` with `Upload-Offset`
  and `application/offset+octet-stream` (≤ 16 MB a piece) → `{offset,
  fertig}`; the piece that makes the file whole starts the import. `GET
  /napi/standort/import/{id}` → `{stand: hochladen|laeuft|fertig|fehler,
  name, groesse, gelesen, format, punkte, besuche, von?, bis?, text?,
  fehler?, grund?}`: `text` is the end in one sentence („12.345 Orte von
  März 2014 bis Dezember 2024 übernommen …“), `fehler` why not (`grund`
  `format`, `keine_orte`, `schluessel_abgelehnt`, `reitti_weg`,
  `kein_schluessel`). One import runs at a time; a Homes restarted
  mid-import goes on at the next GET. The file
  (`HOMES_DATA/standort-import/<person>/<id>.teil`) is deleted when the
  import ends, however; its state is forgotten after seven days.

- The Haushalt MCP source (laib ADR 0017, haus#11): `POST /mcp/haushalt`,
  laib's MCP shape (JSON-RPC over POST: `initialize`, `tools/list`,
  `tools/call`), for the Bote and Claude. Only `Authorization: Bearer
  <HOMES_HAUSHALT_TOKEN>` gets in; without that variable it refuses everyone.
  It is not a page and not behind Caddy: the Bote calls the host port from
  its pod (laib ADR 0012 §4; the template publishes 8650 `lan: false`, the
  firewall keeps the LAN out), so no proxy secret and no Authelia bypass.
  A tool is listed only when its backend is set:
  - `HOMES_RADICALE_URL` + `HOMES_RADICALE_USER` + `HOMES_RADICALE_PASSWORD`
    (CalDAV): `termine(von, bis?)`, `termin_anlegen(titel, beginn, ende?, ort?)`,
    `erinnerungen()`, `erinnerung_anlegen(text, wann)` (a VTODO due then,
    with a display alarm). Reads cover every calendar of the account that
    holds the component (found by PROPFIND). Writes go to one calendar
    only: `HOMES_RADICALE_CALENDAR` (`/<owner>/<collection>/`, read too) when
    set, else the one named `Haushalt`, made on the first write when
    missing. No request goes to a host other than `HOMES_RADICALE_URL`'s.
  - The same Radicale account (CardDAV): `kontakte_suchen(text)` (name,
    nickname, organisation, email, or a number of three digits or more,
    however either side writes it: `0170 …`, `+49 170 …` and `0049 170 …`
    are one number, a `+`/`00` query must match the whole international
    number, any other is a piece of it; the house's calling code is
    `HOMES_PHONE_COUNTRY`, default `49`; ten hits with phone, email and
    birthday),
    `kontakt_lesen(kontakt)` (one contact whole: every number and email,
    ten of a kind, the birthday, the postal addresses (`ADR`, as a letter
    has them) and its page `<HOMES_PUBLIC_URL>/kontakte/<uid>` when
    `HOMES_PUBLIC_URL` is set; found by its UID, else its whole name in any
    case, else the search above; when several match, the answer names ten
    with their UIDs and shows none whole),
    `kontakt_anlegen(name, telefon?, email?, geburtstag?)` (a vCard 4.0
    with a random `urn:uuid` UID; `geburtstag` `JJJJ-MM-TT` or `--MM-TT`),
    `kontakt_ergaenzen(firma, ansprechpartner?, telefon?, email?, iban?,
    quelle?, anschrift?)` (`anschrift` an object `{strasse, plz, ort,
    land?}`, `land` default `DE`) (haus#163, for laib's paperless helper: a firm from a
    document). It finds the card by ORG or FN equal to `firma` (case and
    spaces ignored), else the same email, else the same IBAN (`X-IBAN` or
    one in the note); several at one step narrow to the `CATEGORIES:Firma`
    card, else nothing is written (`mehrdeutig`). None: a new card with
    ORG = FN = firma, `CATEGORIES:Firma`, TEL, EMAIL, `ADR;TYPE=WORK`, `X-IBAN` and one NOTE
    (`Ansprechpartner: …`, `IBAN: …` in groups of four, `Quelle: <link>`).
    One: only what is missing is added — a second TEL or EMAIL, an empty
    contact person, IBAN or address (only when the card has no ADR), the
    source line; a value there is never changed (another address is
    `vorhanden`, the stored one in `gruende`), a different IBAN is never written (`abweichend`, the caller
    marks the document `prüfen`), and a card without `CATEGORIES:Firma`
    (one the family made) gets nothing (`abgelehnt`). The IBAN must pass
    mod 97, a German PLZ five digits; a phone, email, IBAN or address that
    is none is refused for itself.
    The answer is a German line, then one JSON line:
    `{"status": "angelegt|ergaenzt|unveraendert|mehrdeutig", "kontakt":
    "<uid>", "felder": {"firma": …, "telefon": …}, "gruende": {…}}`, each
    given field `angelegt`, `ergaenzt`, `vorhanden`, `abweichend` or
    `abgelehnt`. It writes back only over the ETag it read; a phone's edit
    in between is read again once.
    No other tool changes a contact: until identity per person (below), the
    Bote writes with one account over every address book it can read, and a
    changed card on someone's phone cannot be taken back the way a new one
    can be deleted; `kontakt_ergaenzen` only adds, and only to the firm
    cards Homes made.
    Reads cover every address book of the account; writes go to
    `HOMES_RADICALE_ADDRESSBOOK` when set, else the address book named
    `Haushalt`, made on the first write at `/<user>/haushalt-kontakte/`.
    A read stops at 5000 cards, 64 MB per address book or the 15 s
    timeout (after the first card) and says so. No redirect is followed.
  - `HOMES_PAPERLESS_URL` + `HOMES_PAPERLESS_TOKEN` (a Paperless API token;
    what its user may see is what the Bote sees): `dokumente_suchen(text)`,
    Paperless's own full text search (no index of Homes'), at most 20 hits
    with date, correspondent, title, five tags and, when `HOMES_PUBLIC_URL`
    and `HOMES_PAPERLESS_PUBLIC_URL` are both set, a link to the document's
    page on Homes, `<HOMES_PUBLIC_URL>/dokumente/<id>`, which sends the
    person on to Paperless. Read-only (GET only), no redirect followed, the
    token never in an answer.
  A backend that does not answer (refused, timed out) is a plain tool
  error, `Radicale antwortet gerade nicht; später noch einmal versuchen`
  (or Paperless), its detail in Homes' log; any other answer it did not
  expect says the status.
  - `HOMES_NOTES_DIR` (the share's `notes` folder): `notizen_suchen(text)`,
    `notiz_lesen(titel)`, `notiz_anlegen(titel, text)`. A note is
    `<slug>.md`, `# Titel` first, never written over an existing one (`-2`,
    `-3`; a share without hard links is an error); no name reaches outside
    the folder, hidden files and folders are never notes, a search reads at
    most 5000 files and 64 MB and says when it stopped early.
  - The memory library (haus#13), `HOMES_QMD_URL` with the notes folder:
    `notizen_finden(text)`, the notes by meaning, eight at most, each with
    its title, file, a passage and, with `HOMES_PUBLIC_URL`, its page
    `<HOMES_PUBLIC_URL>/notizen/<file>`; the same cut and tidy-up as the
    page (the tidy-up may take 3 s here: the Bote waits anyway), reduced
    to words the same way, and the answer's first line says so. With the
    documents in the library it finds documents too, in the same eight,
    each as `## Dokument: Titel (Datum, Absender)`, a passage and its page
    `<HOMES_PUBLIC_URL>/dokumente/<id>`; Paperless not answering is the
    answer's first line (*Die Dokumente antworten gerade nicht; gesucht
    wurde nur in den Notizen.*), the notes follow.
  Times are local (`TZ`, e.g. `Europe/Berlin`): `JJJJ-MM-TT` or
  `JJJJ-MM-TTTHH:MM`. Homes keeps nothing of it (ADR 0017 §3): a note is a
  file the family syncs, a reminder an entry on their phones; the one
  index is the memory library's, qmd's, and the word search reads the
  files. Identity per person (the
  person's own calendar, laib ADR 0017 §3) comes next.

- The memory library (haus#13, laib ADR 0017 §3): one search by meaning
  over the notes, for the Notizen page and the Haushalt alike. It is qmd
  (github.com/tobi/qmd), which Laib runs in Homes' pod (laib#195) with its
  own small embedding model on the CPU, refreshed by a timer, behind the
  Vermittler (laib#198), a small server on qmd's SDK that adds each hit's
  cosine. Homes reads its address from `HOMES_QMD_URL`
  (`http://127.0.0.1:<port>`: it has no auth, so it must listen on
  127.0.0.1) and the notes' collection from `HOMES_QMD_COLLECTION`
  (default `notizen`). Homes sends only typed searches, words and meaning,
  with rerank off (a plain query pulls in a 1.7B model and the reranker,
  the slow path), and asks `GET /status` whether the vectors are there;
  qmd gets 2.5 s of a search, the word search the rest. A hit is a note
  only when its path is a plain file name inside the notes folder; what
  qmd names elsewhere, hidden, linked out or deleted is left out. Without
  the notes folder there is no library.
  - **The documents.** Paperless has read every document already (its
    OCR); Homes feeds that text into qmd as its own collection,
    `HOMES_QMD_DOCUMENTS` (e.g. `dokumente`, never the notes' own), which
    counts only with Paperless set. `homes export-documents`, a one-shot
    command the refresh timer runs before `qmd update`, mirrors what
    Homes' Paperless user sees into `HOMES_DOCUMENTS_DIR`: one
    `<id>.md` per document, `# Titel`, then `Datum:`, `Von:` and
    `Schlagworte:`, then the text (at most 2 MB). A file whose time is
    the document's last change is not read again; a document deleted in
    Paperless (or no longer shown to Homes' user) is deleted from the
    folder; Paperless failing leaves the folder as it was and is not the
    refresh's failure (only a folder Homes cannot write is). A search asks
    qmd for both collections; every document it names is checked against
    Paperless in one request (`id__in`, at most 1.5 s, the start of its
    text with it): one deleted since is left out and sets no bar, and
    what a hit shows (title, date, sender, its page) is Paperless's as it
    is now. The cut and the tidy-up cover notes and documents together,
    at most the caller's number of hits in all. When the search by
    meaning cannot answer, or nothing is close, the documents come from
    Paperless's own full text search, any meaningful word of the question
    (`Wort OR Wort`). Paperless not answering leaves the documents out and
    says so; the notes' answer stands.
  - **The cut.** qmd ranks some note first for any question, nonsense
    too, so a hit by meaning is shown only when its cosine is at least
    0.30 and at least 0.75 of the best hit's, in qmd's order (measured on
    44 questions and 10 nonsense ones: 42/44 found, 9/10 nonsense
    emptied). Nothing over the cut: the word search, said as "no note
    fits", not as the search being down (page and Haushalt alike).
  - **The tidy-up**, optional and never blocking: with more than one hit
    left and `HOMES_LLM_URL` (the model proxy's OpenAI base, `…/v1`),
    `HOMES_LLM_MODEL` (the house model, `gemma-4-e4b`) and `HOMES_LLM_KEY`
    all set, Homes reads the lease (`GET /api/model-lease`, no key) and
    asks the house model which hits answer — only when the lease says
    that model is ready, or no window is held and it is the default
    (nobody holds the card; not loaded yet only runs out the budget),
    never taking the lease, never naming
    another model, so it never makes the card swap. One call,
    temperature 0, the answer held to `{"ids":[…]}`, each hit as its
    title and about 700 characters of the note; at most 1.2 s on the
    page, 3 s in the Haushalt. A refusal, no answer in time, an answer
    that is not one, or an empty list keeps the cut's hits: the cut
    already let only close notes through, and the model said "none" when
    a passage missed the answer, not when the note did.

- The Sprechen field (haus#12, laib ADR 0016): on Start under the day,
  above the cards, while the Bote is set (no tab of its own: `/sprechen`
  belongs to Start)
  (`HOMES_BOTE_URL`, the Bote's OpenAI base `http://host.containers.internal:8642/v1`,
  and `HOMES_BOTE_KEY`, Hermes' `API_SERVER_KEY`; laib renders the key only
  with the Bote installed, laib#215: no key, no field, no word about it).
  One line and *Senden*, a plain form post that works without script;
  the page's script (haus#63) sends it in the background instead: from
  Start it hands the sentence to `/sprechen`, which shows at once *Homes
  denkt nach …* (the assistant's name) with a running indicator and the seconds, then the reply
  in pieces as it comes, then the exchange as a reload would show it — or
  the plain line, never silence. `POST /sprechen`
  takes it only from a person on Homes' own page (#35's rule) and sends it
  to the Bote's `/chat/completions` as one chat completion: the person's
  login (`Remote-User`) and, quoted after it without quotes or brackets
  of its own, their name (`Remote-Name`, which a person may set
  themselves) in a system line, because
  Hermes' API server has no per-person field and layers system lines on
  top of the Bote's SOUL, and `X-Hermes-Session-Key: homes:<login>`, the
  person's memory scope. No model named, one sentence at a time: the Bote
  does not see the page's earlier exchange. Homes asks with `stream: true`
  and reads OpenAI's server-sent events (a Bote answering in one piece is
  one piece). A post that accepts `text/event-stream` (the script) gets
  server-sent events back: `{"angekommen":true}` at once, a comment beat
  every 15 s while the Bote thinks, `{"stueck":…}` per piece, and
  `{"fertig":…}`, the exchange's HTML. A plain post gets 303 to
  `/sprechen`, which shows the person's last exchange (in memory, per
  person): their words, the reply under the assistant's name, as text (code
  blocks kept, **bold** bold, nothing else rendered). Homes relays and
  decides nothing (haus#1). Leaving the page does not stop the wait: the
  reply is kept for the person's next look. It waits 150 s for the first
  word (the first message after a model reload takes 40–50 s, with a tool
  more), and a reply under way up to 5 minutes in all; then, with the assistant's name, *Homes hat nach zweieinhalb Minuten
  noch nicht geantwortet. Vielleicht passiert es trotzdem noch: schau nach,
  bevor du es noch einmal schickst.* (a reply cut off keeps what came and
  says *… ist mit der Antwort nicht fertig geworden; hier bricht sie ab.*).
  A Bote down, refusing the key or
  failing its run: *Homes antwortet gerade nicht. Versuch es gleich noch
  einmal.*, the error in the log. Over 1000 characters is refused plainly;
  empty sends nothing.
  The microphone beside the field (haus#63), with Home Assistant and
  `HOMES_STT_PROVIDER` set (HA's speech-to-text, e.g. `stt.faster_whisper`,
  Whisper on the box; `HOMES_STT_LANGUAGE` as the provider names it,
  default `de`); without them, no button. The browser records (Web Audio,
  no browser speech service), at most 30 s, as a 16 kHz 16-bit mono WAV,
  and posts it to `POST /sprechen/hoeren` (a person, Homes' own page, at
  most 2 MB); Homes passes it as it is to HA's `POST /api/stt/<provider>`
  (`X-Speech-Content: format=wav; codec=pcm; sample_rate=…; bit_rate=16;
  channel=1; language=…`) and answers `{ok:true, text}`, which the page
  sends like typed words; nothing is kept. No words is one line
  (`{ok:false, says}`): *Nichts verstanden. …*, *Die Spracherkennung
  antwortet gerade nicht. …* (502, HA's detail in the log), *Die Aufnahme
  ließ sich nicht lesen. …* (400), too long (413).

- The Aktivierungswort page (owner 2026-10-30): `GET /aktivierungswort`,
  only with Home Assistant. The word each voice satellite listens for, as
  the device reports it (its wake-word select, a Voice PE's
  *… Aktivierungswort*). Every speaker is to listen for *„Hey Mycroft“*
  (openWakeWord's pretrained `hey_mycroft`, which a Voice PE offers out
  of the box; Laib selects it, nothing is trained). A device on another
  word gets *„Hey Mycroft“ wird noch ausgewählt*, with the word it hears
  until then; no device in HA, or HA not answering, says „Hey Mycroft“ as
  what is set. Sprechen says the word in one line with a link there.

- The Netzlaufwerk page (haus#52, laib#296): `/netzlaufwerk`, linked from
  Start for a family member or admin (a to-do card while they have no
  password, a quiet link after). Each person sets their own password for the
  network drive (Samba): what it is for, how to connect (`\\<HOMES_SAMBA_HOST>\data`,
  `smb://…/data`), that it may equal their family password. An admin sees
  everyone's state, sets a new one for anyone or removes one. Until a person
  sets one they have none. `POST /netzlaufwerk` takes it only from a person on
  Homes' own page (#35's rule), for themselves or, as an admin, for anyone;
  package `samba` writes one line (`set <name> <base64>`, `clear <name>`) into
  file-share's FIFO in `HOMES_SAMBA_DIR` (non-blocking, never into a plain
  file: no reader is "antwortet gerade nicht") and waits for its `status`.
  The password is never logged, stored or echoed. Unset: no page, no link.

Not yet: Start's *Häufig genutzt*; dictation in Notizen, timers in
Kalender.

- The Box page's Werkstatt card (haus#15): the open tickets of the repos in
  `HOMES_WERKSTATT_REPOS` (owner/name, comma-separated), what pi works
  (`pi:ready`) and what waits for Claude (`needs-claude`), read-only and
  linked. Public GitHub API, no token, cached for 5 minutes. Unset: no card.

## Einkauf (haus#118)

`GET /einkauf`, a pill under Haushalt, with Radicale. The household's one
shopping list is a task-only collection in Homes' account, `homes/einkauf`
("Einkauf"), made at start with the Haushalt calendar, so the phones show
it as a task list too (DAVx⁵ with a task app); it is no calendar and its
tasks are no reminders. A line's text is all there is (a phone's edit is
read the same way): Homes reads amounts the German way — g/kg, ml/l, EL, TL,
Prise, Dose, Packung …, fractions, ranges — adds up the same thing in the
same kind of unit (200 g + 0,5 kg Mehl = 700 g; anything else stands side
by side) and keeps the recipes it came from in DESCRIPTION. A tap ticks a
line off (If-Match: a phone's edit in between is read again, never
overwritten); the ticked ones go below, *Abgehakte entfernen* deletes them.
*Zutaten aus Rezepten* (`/einkauf/rezepte?n=…&portionen=…`): recipe notes
(`art: rezept`, a `## Zutaten` list, "Für N Portionen"), added up and
scaled as one buys (grams to 5, whole tins up), each to leave out before
it goes on the list; a recipe note links there. Every change is pushed
over `/live` (`homes.einkauf`) to the other open pages; a phone app's change
shows within a minute. The assistant: `einkaufsliste_dazu(dinge, rezepte,
portionen)` and `einkaufsliste`.

## Musik-Wunschliste (owner 2026-10-25)

A song or an album shared to Homes — the app's share target, *Aus Link
merken* on Notizen, a link sent to the assistant (`notiz_aus_link`) — goes on the
household's list *Musik* (`haus/liste-musik`, made at start) instead of
into a note: "Künstler – Titel", its origin `von <person>, <link>`. A
YouTube video counts when YouTube's oEmbed says it is a music video
(Official Video/Audio, Lyric Video, a VEVO or "- Topic" channel, YouTube
Music); Spotify tracks and albums by their page. Its lines are never read
as amounts. On the list each open wish shows who wished it, shops that sell
music as files (Bandcamp, Qobuz, 7digital) and the link to listen to.
With Jellyfin (`HOMES_JELLYFIN_URL`, `HOMES_JELLYFIN_TOKEN`, laib#695) a song
already in the music is "Hast du schon … auf dem Album …", and every 15
minutes the open wishes are looked up: one that arrived is ticked off and
its wisher told (`laib_tell`). Nothing is downloaded from YouTube.

Where a wish is to be had comes from MusicBrainz (no key, one request a
second, in the background after the wish and again hourly when it did not
answer): the album a song is on and when it comes out ("Erscheint am …:
vorbestellen"), its label, and the shops the release, the band and the
label name — Bandcamp first (a record there mostly comes with its download
at once), mail order, the band's and the label's pages, known download
shops. Shops are never asked themselves (Bandcamp lets no program in).
Each wish moves through stages kept in `musikwuensche.json` in HOMES_DATA:
*Wunsch* → *Bestellt* (a record with its download, or the file alone) →
*Datei da* (the record on its way; set by hand, or by Jellyfin when the
download is sorted in) → ticked off when all of it is there.

## Discogs: each person's shelf and wantlist, Platte erfassen (owner 2026-10-25/26)

One Discogs account per person (owner: "es muss doch ein Schlüssel pro
User sein"), connected through the household's Discogs app ("Joshua";
its consumer key and secret from laib: `HOMES_DISCOGS_KEY`,
`HOMES_DISCOGS_SECRET`, laib#696). *Discogs* under Mehr (`/discogs`): "Mit
Discogs verbinden" sends the person to Discogs, which asks whether the
app may use their account, and back to `/discogs/zurueck`; Homes keeps
their token in `discogs-konten.json` (HOMES_DATA, 0600, never shown) for
them alone, and only for the connection it started for them (OAuth 1.0a,
PLAINTEXT over https). Once connected the page leads with *Platte
erfassen*, then *Verbunden mit „…“ seit …*, and ends with a quiet, asked
*Trennen*, which forgets it. Per person then: a wish already
on their shelf is "Hast du schon auf Vinyl"; *Auf die Discogs-Wantlist*
puts a wish's album on the wantlist of who presses it; a wish whose album
arrives on its wisher's shelf is ticked off and leaves the wantlist.
*Platte erfassen* (`/platte`): 1–4 photos of a record. The phone makes
them small and reads a barcode with the browser's `BarcodeDetector`;
without a barcode the box's model reads catalogue number, label, artist
and title (`HOMES_LLM_VISION_MODEL`, else the house model; only while it has
the card, else who has it). Discogs is searched by barcode (its check digit
checked), then catalogue number and label, then artist and title on vinyl;
the best five show with their cover (through Homes, `/platte/bild`) and why
they match, and only *Das ist sie* adds one to the person's collection.

## Podcasts (haus#123)

`GET /podcasts`, under Mehr, with Audiobookshelf (`HOMES_ABS_URL`,
`HOMES_ABS_TOKEN`: an admin user's API key from laib, laib#676). Anyone in
the family subscribes by a link — the feed, a website (its
`<link rel=alternate type=application/rss+xml>`), Apple Podcasts (its id
through Apple's lookup), a Spotify show (its name through Apple's
search; a Spotify-only show is said to have no feed) — fetched by
`internal/merken`'s rules. Homes asks ABS to read the feed, subscribes it
once for the household (`POST /api/podcasts`, auto-download) and sets it
to keep only the last 3 episodes (owner 2026-10-25); `podcasts.json` in
HOMES_DATA keeps who subscribed to what. *Meine Podcasts* with *Abo
beenden* (the last one out takes the podcast off the box), their newest
episodes. The assistant: `podcast_abonnieren(link, person)`. Every 15 minutes
Homes looks for new episodes and tells each only to who subscribed,
through Laib's `tell` (laib#676) — not before Laib has it, so nothing is
taken as told unsaid.

## Musik (haus#107; owner 2026-11-04: „Musik verwalten“)

`GET /musik`, under Mehr, only with laib's music inbox: `HOMES_MUSIC_INBOX`,
else `/musik-eingang` when it is mounted (laib#639's homes template mounts
it there; beets sorts it into the music and Jellyfin). Managing the
household's music, not what is playing, and only its files (owner: „nur
Dateien darstellen, aber nichts Komplexes“; what came in last is the music
app's or Jellyfin's). One h1, then:

- **Hinzufügen**: how music gets in — on the phone, share songs or an
  album as a ZIP to Homes (`POST /napi/musik`); with the network drive
  (`HOMES_SAMBA_DIR`), at home on a computer into `musik-eingang` on the
  share `data`, linked to `/netzlaufwerk`. Under it one list of the files,
  a row each: „wird einsortiert …“ for what is at the inbox's top or in its
  `.in-arbeit/` (the page refreshes itself every 10 s meanwhile, and says
  after ten minutes that it takes longer and a message follows); „nicht
  einsortiert: <reason>“ for what beets kept back (`_nicht-importiert/<run>/`,
  `GRUND.txt`), with „Noch einmal einsortieren“ (back into the inbox, the
  person's sidecar beside it) and „Löschen“ (asked; 30 days in the inbox's
  hidden `.papierkorb`, Rückgängig right after) on its row; „fertig: 2 Alben,
  24 Titel in der Musik“ for the last week's uploads from beets' own status
  in laib (`letzte`, at most five).
- **Platten**, by whether one's Discogs is connected: connected, „Platte
  erfassen“ and „Wunschliste · N“ (the music wish list's open wishes), and
  last and quiet „Discogs: verbunden mit …“; not connected, the wish list
  and the quiet „Mit Discogs verbinden“ with what it brings. The wish list
  is Homes' own (Radicale); Discogs only adds the wantlist. Without the
  household's Discogs app, an admin sees the way to its keys.

## The Homes script

Every family page carries one inline script in its head (`hausScript`,
`internal/pages/skript.go`; owner 2026-10-24: scripts are welcome). A POST
form goes in the background: the pressed switch flips at once, a button
with `data-laeuft` says what runs, the answer replaces the page in place
(scroll, focus and open folds kept, the address follows a redirect). A
refusal the server says as `text/plain` (`/schalten` does when asked with
`X-Homes-Skript`) is shown at the bottom and the page stays. `data-frage`
asks before a removal, `data-kopieren` copies the field before it, a
`<div hidden data-auffrischen=N>` refreshes the page in place every N
seconds while it is seen and nobody types (Energie 60, Dokumente 2–5
while a scan moves). Pages that show Home Assistant's things are pushed
instead (owner 2026-10-24): with `data-live` the script listens on
`GET /live` (server-sent events, behind Authelia like every page; one
`state_changed` subscription to HA while anyone listens, sensors left
out) and fetches the page in place as soon as a thing on it changes —
Räume, a thing, Medien (`seite`), Start any thing (`alle`); their N=120
is only the fallback and for the day's calendar and the box line. `data-ganz`
leaves a form to the browser.
