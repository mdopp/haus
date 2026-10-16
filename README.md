# Haus

**Haus** is the family's surface for the box that [Laib](https://github.com/mdopp/laib)
runs: pages for people, cards, and `/napi`, the API the widgets of the Haus
app ([mdopp/haus-android](https://github.com/mdopp/haus-android)) speak.
Haus renders and decides nothing; Laib acts; the Bote is who you talk to,
and messages to people go through their chat channel, not the app (laib ADR
0014, 0016, 0022). The family sees the house persona (laib ADR 0021). The
app's names (scheme `cloud.dopp.haus`, token prefix
`haus_device_`, the Box keys under `/napi/box`) live in `internal/app`; a
`/napi` path wants a paired device (`Authorization: Bearer haus_device_…`,
else 401, the unknown paths too); one Haus does not serve, or whose backend
is not set, then answers 404 `{"ok":false,"reason":"not_found"}`.

## This cut (laib#28)

- `GET /start` — the family's page behind Authelia (haus#12, haus#35):
  *Zuhause* and the day, then one part per question a family member has.
  *Im Haus*: who is home (`person.*`: "Anna und Ben sind da"), the lamps
  that are on with their names and one button (*Aus* for one, *Alle aus*
  for several; a group, any entity with an `entity_id` list, is not a
  lamp), what plays on a `media_player` with *Pause* when it can pause.
  *Meine Geräte*: the person's own things as tiles like the phone widget
  (laib ADR 0017: favorites go to Haus): `HAUS_DATA/favoriten.json`,
  `{"<Remote-User>": ["light.buero", "cover.wohnzimmer", …]}`, read on each
  page load, in the person's order, at most twelve, the state in a word
  and a bar, a lamp's or a switch's switch on the tile; what a thing's page
  would not show (not a card domain, unknown to HA, hidden by HA) is left
  out. None says how to get one (*Tippe in Räume bei einem Gerät auf ☆,
  dann erscheint es hier.*); no `Remote-User`, no store: no part.
  *Heute und demnächst*: appointments and open reminders of the next 14
  days together, by time (overdue first, one without a date last,
  *offen*), at most six, each leading to its page; none says so and how it
  fills. Notes are not on Start: they have their tab.
  The box is one calm line at the bottom (*Alles in Ordnung · eure Daten
  sind von gestern Abend gesichert*; without *Alles in Ordnung* when a part
  of the page does not answer); a service down or the data not backed up
  for three days moves it to the top, saying what it means (*Gerade nicht
  erreichbar: Fotos.*, *Eure Daten sind seit 3 Tagen nicht gesichert.*) and
  who is on it: amber when the box mends it itself, red only when a person
  is needed (nobody automatic on it, or the data not backed up). The admin's words (services, places, the last change,
  Rückgängig) are /box's.
  Each part only when its backend is set (`HAUS_HA_URL` + `HAUS_HA_TOKEN`,
  `HAUS_RADICALE_*`); one not set is not mentioned. One not
  answering within 4s is one plain line where its rows would be (*Licht
  und Geräte antworten gerade nicht. Schalten geht im Moment nicht.*, *Der
  Kalender antwortet gerade nicht.* once, a line in *Meine Geräte* when its
  file or HA does not), never its name or its error, which go to Haus's
  log; Laib not answering is *Wie es der Box geht, lässt sich gerade nicht
  sagen.* at the bottom. The page is always a 200. Cards are one renderer,
  `internal/cards` (title, rows, optional link; a list, boxes, tiles or a
  time column; a row may carry one button), for the pages and later the
  widgets' JSON. `/` redirects to `/start`; every other page path Haus
  does not serve is a 404 in the family's frame: *Diese Seite gibt es
  nicht.* and *Zur Startseite* (haus#12).
- The family's page frame (haus#35): the house palette (the chat's tokens
  and haus-android's `colors.xml`: ground `#070709`, cards `#14141A`,
  orange `#F97316` for what you can do, amber `#FFC107` for what is on,
  green and red only for a state, never alone), dark or light as the phone
  is, Geist served by Haus itself at `/schrift/…woff2` (OFL, vendored in
  `internal/pages/schrift`; no request to Google, the phone's own font
  when it does not load), a tab bar with icons at the bottom of every
  family page (Start · Räume · Kalender · Notizen, Box for admins). No
  persona on the family's pages: it speaks where you talk to it.
- `POST /schalten` — a family page's button (haus#35): `geraet`, `tun`
  (`light|switch.turn_on|turn_off`, `media_player.media_pause`, or
  `alle-lichter-aus`: every lamp *Im Haus* counts) and `zurueck` (Start or
  a thing's page; anything else is Start). Each call passes the widgets'
  own rule (`internal/schalter`, below); nothing else in the form rides
  along. Only with `Remote-User` and from Haus's own page
  (`Sec-Fetch-Site: same-origin`, or, from an older browser, an `Origin`
  of Haus's host; neither header is a 403 too), else 403; then 303 back. HA not answering is one plain
  line and the way back. No script: a plain form.
- `POST /raeume/<id>` `stern=an|aus` — the ☆ on a thing's own page pins it
  to the person's *Meine Geräte* or takes it off (`favoriten.json`, their
  own list only, at most 50, the file replaced whole, a file that does not
  read never written over); same guards as `/schalten`, then 303 back.
- `GET /raeume`, `/kalender`, `/notizen` — the family's other pages
  (haus#12), from the same cards, in the family's frame. A page whose
  backend is not set lands on Start; one
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
    the ☆ pins it to Start.
  - *Kalender*: *Heute* (the day's appointments with time and place),
    *Nächste 14 Tage*, *Erinnerungen* (open reminders, overdue said so; one
    due on a day, not at a time, shows the day and is overdue the day after),
    every calendar of the Radicale account, at most 50 rows a card. Each
    reminder links to its own page. Radicale not answering: *Der Kalender
    antwortet gerade nicht.* once, where the appointments would be, or in
    *Erinnerungen* when only the reminders fail.
  - *Notizen*: a search field (`GET /notizen?q=…`, the memory library
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
  - The item pages (haus#28), what a widget opens on a tap: `/raeume/<entity_id>`
    (a thing: its state in words, since when, its room; only an entity of
    a card domain, `cards.Domains`, that HA does not hide — a person, a
    tracker, a hidden entity are a 404),
    `/kalender/erinnerung/<uid>` (a reminder: open and due, done, or
    cancelled), `/kontakte/<uid>` (a contact: numbers and email addresses to
    tap, the birthday, the postal addresses), `/dokumente/<id>` (a 302 to the document's page in
    Paperless, `HAUS_PAPERLESS_PUBLIC_URL`; without it a 404), and a note's
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
  the `admins` group. Haus itself sends anyone whose `Remote-Groups` lacks
  `admins` from `/box` and everything under it (Geräte included) to
  `/start` with 303, since Authelia here only restricts per host. That
  says who sees the admin page; it is not a verdict on anything, and
  `/napi` and `/pair-device` are untouched. The page: readiness, the inbox read-only
  with its reasons and a link to Laib's own inbox (the one approval
  surface, laib ADR 0012), what runs, the journal. Every page request must
  carry `X-Haus-Proxy: <HAUS_PROXY_SECRET>`, which only Caddy adds; the
  loopback port is shared with every host-network container, so without
  the secret nobody is anybody.
- `/napi/box/{home,services,approvals,upgrades}` — the four Box keys the
  widgets read, from `laib status`, `laib inbox`. `pendingUpdates` and
  `upgrades` are the open proposals of kind `template-update` (`laib
  template sync --propose` files one per update); `pendingApprovals` and
  `approvals` are the others. An update is counted once, as an update. `POST …/services/{name}/operate`
  (restart). `POST …/approvals/{id}/approve|deny` answers 403: Haus decides
  nothing (haus#1); approvals reach the admin through the relay (laib ADR 0012).

- Pairing: in the app, enter `https://haus.<domain>`. The app opens
  `/pair-device` in a Custom Tab, Authelia logs the person in, they confirm,
  and Haus redirects to `cloud.dopp.haus://pair#token=haus_device_…&id=…`,
  the token in the fragment (haus#6). `GET /box/geraete` lists the devices,
  revocable, and can mint a token whose setup URI
  (`cloud.dopp.haus://setup?server=…&token=…`) goes into a QR code the app
  scans. That URI is QR content, not a link: the app only opens `…://pair`.
  Tokens live hashed in `HAUS_DATA/devices.json`; `/napi/whoami` answers for
  them. Only a bearer with `haus_device_` is a device: a phone paired before
  the Haus app is not migrated, it pairs again (an old entry stays listed
  under Geräte until revoked).

- The house persona (laib ADR 0020): Laib mounts `render/persona` at
  `/persona` and sets `HAUS_PERSONA`. The admin's pages (/box) carry its
  name in the title, a header with its icon, name and greeting in its
  colours, and `/persona/icon.svg`; the family's pages do not (haus#35). The app reads `GET /napi/persona` (display, greeting,
  accent, ground, icon) and `/napi/persona/icon.png` with its device token:
  PNG, which a phone draws without a library (haus#28). It is the persona's
  `icon.png` (laib#191); a persona without one has no icon on the phone.
  Without a persona Haus is plain Haus; internally it is always `haus`.

- The house paths, from Home Assistant (haus#10, set by `HAUS_HA_URL` +
  `HAUS_HA_TOKEN`, a long-lived token; unset, they are 404):
  `POST /napi/ha/call` (`{entity_id, service, data?, confirmed?}`; only
  `light|switch|cover|climate|lock`, and `media_player.media_pause` (haus#35;
  any other player service is 400 `unsupported_service`), else 400; the
  rule is `internal/schalter`, the family's pages' too; the service's domain is the
  entity's; `data` cannot name another target; any `lock`, and on a
  garage/door/gate cover — or one whose `device_class` cannot be read —
  anything but `close_cover`, `stop_cover`, `close_cover_tilt`,
  `stop_cover_tilt`, answers **403 `sensitive_action`** without
  `confirmed:true`; HA refusing the call is 400, HA in trouble is 502
  `ha_unavailable`, HA's detail stays in Haus's log) and
  `GET /napi/portal/{state,energy,entity-history,active}`. Every one wants
  a paired device. The shapes are the ones the app's `data/ApiClient.kt` parses; no JSON `null` goes out
  (Android reads it as `"null"`). Every card names its thing's page
  (`page`, `/raeume/<entity_id>`).
- The energy picture reads the entities `HAUS_ENERGY_FLOW`
  (`pv=sensor.…,house=sensor.…,grid=sensor.…,battery=sensor.…`, any subset)
  and `HAUS_ENERGY_TOTALS` (`<Label>=sensor.…,…`, in that order) name;
  every other power sensor is a circuit. Unset, it falls back to matching
  one house's German friendly names (`Aktuell erzeugter PV-Strom`, …).
- The realtime pair `POST /napi/portal/watch` + `GET /napi/portal/events`,
  from Home Assistant too. The stream is SSE and carries state changes only
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
- The tool widgets, from the Haushalt source's backends (haus#10, the
  same variables as below; none set, they are 404): `GET /napi/defs/tool`
  lists a tool per backend that is set: `task` (*Erinnerungen*,
  `HAUS_RADICALE_*`), `contacts`, `doc` (`HAUS_PAPERLESS_*`), `note`
  (`HAUS_NOTES_DIR`). Each def names its
  row path (`/napi/portal/tasks|contacts|documents|notes`: the open
  reminders, soonest first, due in house time, each with `status` and
  its UID trimmed as `id`, and with `?done=1` the done (`done`) and
  cancelled (`dismissed`) ones after them, the latest first; contacts by
  name with the first number and address; the newest Paperless
  documents; the newest notes; at most 40 rows, no JSON `null`, a
  backend in trouble 502). Each row with an id names its item page
  (`page`, above); a document only when Paperless's public address is
  set. No def names a create page: Haus has none. A path whose backend is
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
  phone unticks it), so it asks no `confirmed`; Haus adds, files or
  changes nothing else from a row.
- The camera capture: `POST /napi/upload` (multipart, `file` parts, an
  optional `filename`) stores into `HAUS_UPLOAD_DIR`, the share's folder
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
  several, with no `url` (Haus serves no file back). Limits: 507 when the
  share has less than 128 MB free or runs full, 429 while two uploads
  run, 408 when a body stalls past two minutes; a disk error is 500
  `store_failed`, not the phone's fault.

- The Haushalt MCP source (laib ADR 0017, haus#11): `POST /mcp/haushalt`,
  laib's MCP shape (JSON-RPC over POST: `initialize`, `tools/list`,
  `tools/call`), for the Bote and Claude. Only `Authorization: Bearer
  <HAUS_HAUSHALT_TOKEN>` gets in; without that variable it refuses everyone.
  It is not a page and not behind Caddy: the Bote calls the host port from
  its pod (laib ADR 0012 §4; the template publishes 8650 `lan: false`, the
  firewall keeps the LAN out), so no proxy secret and no Authelia bypass.
  A tool is listed only when its backend is set:
  - `HAUS_RADICALE_URL` + `HAUS_RADICALE_USER` + `HAUS_RADICALE_PASSWORD`
    (CalDAV): `termine(von, bis?)`, `termin_anlegen(titel, beginn, ende?, ort?)`,
    `erinnerungen()`, `erinnerung_anlegen(text, wann)` (a VTODO due then,
    with a display alarm). Reads cover every calendar of the account that
    holds the component (found by PROPFIND). Writes go to one calendar
    only: `HAUS_RADICALE_CALENDAR` (`/<owner>/<collection>/`, read too) when
    set, else the one named `Haushalt`, made on the first write when
    missing. No request goes to a host other than `HAUS_RADICALE_URL`'s.
  - The same Radicale account (CardDAV): `kontakte_suchen(text)` (name,
    nickname, organisation, email, or a number of three digits or more,
    however either side writes it: `0170 …`, `+49 170 …` and `0049 170 …`
    are one number, a `+`/`00` query must match the whole international
    number, any other is a piece of it; the house's calling code is
    `HAUS_PHONE_COUNTRY`, default `49`; ten hits with phone, email and
    birthday),
    `kontakt_lesen(kontakt)` (one contact whole: every number and email,
    ten of a kind, the birthday, the postal addresses (`ADR`, as a letter
    has them) and its page `<HAUS_PUBLIC_URL>/kontakte/<uid>` when
    `HAUS_PUBLIC_URL` is set; found by its UID, else its whole name in any
    case, else the search above; when several match, the answer names ten
    with their UIDs and shows none whole),
    `kontakt_anlegen(name, telefon?, email?, geburtstag?)` (a vCard 4.0
    with a random `urn:uuid` UID; `geburtstag` `JJJJ-MM-TT` or `--MM-TT`).
    No tool changes a contact: until identity per person (below), the Bote
    writes with one account over every address book it can read, and a
    changed card on someone's phone cannot be taken back the way a new
    one can be deleted.
    Reads cover every address book of the account; writes go to
    `HAUS_RADICALE_ADDRESSBOOK` when set, else the address book named
    `Haushalt`, made on the first write at `/<user>/haushalt-kontakte/`.
    A read stops at 5000 cards, 64 MB per address book or the 15 s
    timeout (after the first card) and says so. No redirect is followed.
  - `HAUS_PAPERLESS_URL` + `HAUS_PAPERLESS_TOKEN` (a Paperless API token;
    what its user may see is what the Bote sees): `dokumente_suchen(text)`,
    Paperless's own full text search (no index of Haus's), at most 20 hits
    with date, correspondent, title, five tags and, when `HAUS_PUBLIC_URL`
    and `HAUS_PAPERLESS_PUBLIC_URL` are both set, a link to the document's
    page on Haus, `<HAUS_PUBLIC_URL>/dokumente/<id>`, which sends the
    person on to Paperless. Read-only (GET only), no redirect followed, the
    token never in an answer.
  A backend that does not answer (refused, timed out) is a plain tool
  error, `Radicale antwortet gerade nicht; später noch einmal versuchen`
  (or Paperless), its detail in Haus's log; any other answer it did not
  expect says the status.
  - `HAUS_NOTES_DIR` (the share's `notes` folder): `notizen_suchen(text)`,
    `notiz_lesen(titel)`, `notiz_anlegen(titel, text)`. A note is
    `<slug>.md`, `# Titel` first, never written over an existing one (`-2`,
    `-3`; a share without hard links is an error); no name reaches outside
    the folder, hidden files and folders are never notes, a search reads at
    most 5000 files and 64 MB and says when it stopped early.
  - The memory library (haus#13), `HAUS_QMD_URL` with the notes folder:
    `notizen_finden(text)`, the notes by meaning, eight at most, each with
    its title, file, a passage and, with `HAUS_PUBLIC_URL`, its page
    `<HAUS_PUBLIC_URL>/notizen/<file>`; the same cut and tidy-up as the
    page (the tidy-up may take 3 s here: the Bote waits anyway), reduced
    to words the same way, and the answer's first line says so. With the
    documents in the library it finds documents too, in the same eight,
    each as `## Dokument: Titel (Datum, Absender)`, a passage and its page
    `<HAUS_PUBLIC_URL>/dokumente/<id>`; Paperless not answering is the
    answer's first line (*Die Dokumente antworten gerade nicht; gesucht
    wurde nur in den Notizen.*), the notes follow.
  Times are local (`TZ`, e.g. `Europe/Berlin`): `JJJJ-MM-TT` or
  `JJJJ-MM-TTTHH:MM`. Haus keeps nothing of it (ADR 0017 §3): a note is a
  file the family syncs, a reminder an entry on their phones; the one
  index is the memory library's, qmd's, and the word search reads the
  files. Identity per person (the
  person's own calendar, laib ADR 0017 §3) comes next.

- The memory library (haus#13, laib ADR 0017 §3): one search by meaning
  over the notes, for the Notizen page and the Haushalt alike. It is qmd
  (github.com/tobi/qmd), which Laib runs in Haus's pod (laib#195) with its
  own small embedding model on the CPU, refreshed by a timer, behind the
  Vermittler (laib#198), a small server on qmd's SDK that adds each hit's
  cosine. Haus reads its address from `HAUS_QMD_URL`
  (`http://127.0.0.1:<port>`: it has no auth, so it must listen on
  127.0.0.1) and the notes' collection from `HAUS_QMD_COLLECTION`
  (default `notizen`). Haus sends only typed searches, words and meaning,
  with rerank off (a plain query pulls in a 1.7B model and the reranker,
  the slow path), and asks `GET /status` whether the vectors are there;
  qmd gets 2.5 s of a search, the word search the rest. A hit is a note
  only when its path is a plain file name inside the notes folder; what
  qmd names elsewhere, hidden, linked out or deleted is left out. Without
  the notes folder there is no library.
  - **The documents.** Paperless has read every document already (its
    OCR); Haus feeds that text into qmd as its own collection,
    `HAUS_QMD_DOCUMENTS` (e.g. `dokumente`, never the notes' own), which
    counts only with Paperless set. `haus export-documents`, a one-shot
    command the refresh timer runs before `qmd update`, mirrors what
    Haus's Paperless user sees into `HAUS_DOCUMENTS_DIR`: one
    `<id>.md` per document, `# Titel`, then `Datum:`, `Von:` and
    `Schlagworte:`, then the text (at most 2 MB). A file whose time is
    the document's last change is not read again; a document deleted in
    Paperless (or no longer shown to Haus's user) is deleted from the
    folder; Paperless failing leaves the folder as it was and is not the
    refresh's failure (only a folder Haus cannot write is). A search asks
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
    left and `HAUS_LLM_URL` (the model proxy's OpenAI base, `…/v1`),
    `HAUS_LLM_MODEL` (the house model, `gemma-4-e4b`) and `HAUS_LLM_KEY`
    all set, Haus reads the lease (`GET /api/model-lease`, no key) and
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
  (`HAUS_BOTE_URL`, the Bote's OpenAI base `http://host.containers.internal:8642/v1`,
  and `HAUS_BOTE_KEY`, Hermes' `API_SERVER_KEY`; laib renders the key only
  with the Bote installed, laib#215: no key, no field, no word about it).
  One line and *Senden*, a plain form post, no script. `POST /sprechen`
  takes it only from a person on Haus's own page (#35's rule) and sends it
  to the Bote's `/chat/completions` as one chat completion: the person's
  login (`Remote-User`) and, quoted after it without quotes or brackets
  of its own, their name (`Remote-Name`, which a person may set
  themselves) in a system line, because
  Hermes' API server has no per-person field and layers system lines on
  top of the Bote's SOUL, and `X-Hermes-Session-Key: haus:<login>`, the
  person's memory scope. No model named, no stream, one sentence at a time:
  the Bote does not see the page's earlier exchange. Then 303 to
  `/sprechen`, which shows the person's last exchange (in memory, per
  person): their words, the reply under the persona's name, as text (code
  blocks kept, **bold** bold, nothing else rendered). Haus relays and
  decides nothing (haus#1). Leaving the page does not stop the wait: the
  reply is kept for the person's next look. It waits 60 s; then, in the house persona's name, *Joshua hat nach einer Minute
  noch nicht geantwortet. Vielleicht passiert es trotzdem noch: schau nach,
  bevor du es noch einmal schickst.* A Bote down, refusing the key or
  failing its run: *Joshua antwortet gerade nicht. Versuch es später noch
  einmal.*, the error in the log. Over 1000 characters is refused plainly;
  empty sends nothing.

Not yet: Start's *Häufig genutzt*; dictation in Notizen, timers in
Kalender.

- The Box page's Werkstatt card (haus#15): the open tickets of the repos in
  `HAUS_WERKSTATT_REPOS` (owner/name, comma-separated), what pi works
  (`pi:ready`) and what waits for Claude (`needs-claude`), read-only and
  linked. Public GitHub API, no token, cached for 5 minutes. Unset: no card.

## What the family uses (haus#50)

Haus counts, server-side and anonymously, what the family uses, so that
what gets built next is decided on numbers (owner decision 2026-10-16):
a count per event and **UTC day**, nothing else. Never who: no
`Remote-User`, no device token, no IP, no content, no search words, no
entity — a request's query is read only for the widget source tag below.
The counts go to **usage-metrics** (mdopp/usage-metrics, a Laib built-in,
`POST /ingest`, app `haus`) with `HAUS_USAGE_URL` and `HAUS_USAGE_TOKEN`
(laib's `templates/haus` wires both; unset, Haus counts nothing and the
Box card is absent). Counting is a map increment in memory; a flush every
five minutes sends one increment per counter, and SIGTERM sends what is
left before Haus exits. usage-metrics not answering drops that batch and
says so once in the log; a page never waits for it.

| Event | Counted when | The card says |
|---|---|---|
| `seite.<page>` | a page is shown (GET, 200; behind the proxy secret): `start`, `raeume`, `ding` (a thing's page), `energie`, `medien`, `kalender`, `erinnerung`, `kontakt`, `dokument`, `notizen`, `notiz`, `sprechen`, `box`, `geraete`, `koppeln` | *Start geöffnet*, … |
| `schalten.<domain>.<service>`, `schalten.alle-lichter-aus` | a page's switch past its guards (`light.turn_on` … `media_player.volume_up`) | *Licht ausgeschaltet*, *Alle Lichter ausgeschaltet*, … |
| `stern.an`, `stern.aus` | the ☆ past its guards | *Ein Gerät auf Start geheftet (☆)* |
| `suche.notizen` | a search on Notizen (never its words) | *In den Notizen gesucht* |
| `sprechen.senden` | a sentence sent in the Sprechen field (never what) | *Im Sprechen-Feld geschrieben* |
| `gestoert.<page>.<line>` | a page showed a plain line because something did not answer: `licht`, `raeume`, `kalender`, `kontakte`, `notizen`, `bedeutung`, `dokumente`, `favoriten`, `favoriten-verdeckt`, `strom`, `strom-heute`, `lautsprecher`, `schalten`, `bote`, `bote-langsam`, `box`, `laib`, `github`, `dienste` | the line itself and where: *„Licht und Geräte antworten gerade nicht.“ auf Start* |
| `napi.<path>` | a paired phone's `/napi` request, by path without wildcards (`napi.portal.state`, `napi.box.home`, `napi.ha.call`, …) | *Geräte-Widget aktualisiert*, … |
| `von.widget.<kind>` | a page opened from a widget, `?von=widget.<kind>` (haus-android#181): `device`, `devices`, `active`, `energy`, `energy-stats`, `energy-totals`, `box-overview`, `box-services`, `tool`; any other value counts nothing | *Vom Widget „Gerät“ aus geöffnet* |

**The voice control** (Home Assistant Assist), app `stimme`, when HA is
configured: HA keeps the last ten runs of each Assist pipeline in memory
(its debug store, `assist_pipeline/pipeline_debug/list|get` over the
WebSocket); Haus reads them every minute and counts each run once:
`erkannt` (HA's own intents answered), `weitergereicht` (the fallback
conversation agent answered), `nicht-erkannt` (the answer was an error,
e.g. `no_intent_match`). The sentence of a `nicht-erkannt` run goes to
usage-metrics' `POST /miss` (source `stimme`) — the words alone, never the
device, satellite or conversation; usage-metrics keeps it 30 days. Runs
from before Haus started are not counted, so a restart counts nothing
twice; more than ten commands on one pipeline within a minute lose the
oldest. **These commands are admin-only in HA**: the user behind
`HAUS_HA_TOKEN` must be an administrator, else Haus logs it once, counts
no voice, and the card says so.

**The Box page's card "Was wird genutzt"** (admins only): the last 30
days from usage-metrics' `GET /summary?days=30` and `GET /misses?days=30` —
the five most used things, what this box has and nobody or hardly anybody
used (*nie*), which plain lines the family saw where, the Bote's turns,
unanswered turns and most used tools (app `bote`: `zug`, `unbeantwortet`,
`werkzeug.<tool>`, counted by laib's Bote template) and the voice's
numbers, and the sentences nobody could answer (*im Chat*, *per Stimme*;
kept 30 days, without names). usage-metrics not answering is one plain
line; nothing counted yet says so and since when Haus counts.

Before: ServiceBay's Solaris widgets tagged their links `?src=widget.*`
and the Solaris engine forwarded those hits to usage-metrics
(solarisbay#1026), nothing else was counted. Now the app only tags the
link (`?von=`), and Haus counts server-side itself — pages, actions,
degraded lines, widget refreshes and the voice — under the same store and
retention.

## Run

```
LAIB_URL=http://host.containers.internal:5900 LAIB_TOKEN=<the haus hand's token, minted on laib add haus> \
HAUS_PROXY_SECRET=<what Caddy sends as X-Haus-Proxy> LAIB_PUBLIC_URL=https://laib.<domain> \
HAUS_LISTEN=0.0.0.0:8650 \
HAUS_HA_URL=http://host.containers.internal:8123 HAUS_HA_TOKEN=<a long-lived HA token> \
HAUS_ENERGY_FLOW=pv=sensor.…,house=sensor.…,grid=sensor.…,battery=sensor.… HAUS_ENERGY_TOTALS=PV-Erzeugung=sensor.…,… \
HAUS_HAUSHALT_TOKEN=<the Bote's bearer> TZ=Europe/Berlin \
HAUS_RADICALE_URL=http://host.containers.internal:5232 HAUS_RADICALE_USER=<service account> HAUS_RADICALE_PASSWORD=<its password> \
HAUS_PAPERLESS_URL=http://host.containers.internal:8000 HAUS_PAPERLESS_TOKEN=<a Paperless API token> \
HAUS_PAPERLESS_PUBLIC_URL=https://paperless.<domain> HAUS_NOTES_DIR=/notes \
HAUS_QMD_URL=http://127.0.0.1:8181 HAUS_QMD_DOCUMENTS=dokumente HAUS_DOCUMENTS_DIR=/dokumente HAUS_UPLOAD_DIR=/consume \
HAUS_LLM_URL=http://host.containers.internal:11435/v1 HAUS_LLM_MODEL=gemma-4-e4b HAUS_LLM_KEY=<the model key> \
HAUS_BOTE_URL=http://host.containers.internal:8642/v1 HAUS_BOTE_KEY=<the Bote's API_SERVER_KEY> \
HAUS_USAGE_URL=http://host.containers.internal:8094 HAUS_USAGE_TOKEN=<usage-metrics' USAGE_METRICS_TOKEN> ./haus
```

`HAUS_DATA` (default `/data`) holds `devices.json` and `favoriten.json`; `HAUS_PUBLIC_URL` is the
origin the setup link carries (default `https://<Host>`). Haus's Laib token is the `haus` hand: status, inbox, journal, restart —
nothing else (no exec, no write). There are no static device tokens. Image: `ghcr.io/mdopp/haus`. Laib template: `haus` (route `haus.<domain>`,
`bypass: [/napi/]`; the
upload needs the share's consume folder mounted writable at
`HAUS_UPLOAD_DIR`, on a file system with hard links, and Caddy letting a
request of 64 MB through).

`haus export-documents` runs once instead of the server and exits (the
refresh timer runs it in Haus's container, `podman exec systemd-haus
/haus export-documents`, before `qmd update`): it reads
`HAUS_PAPERLESS_URL`, `HAUS_PAPERLESS_TOKEN` and `HAUS_DOCUMENTS_DIR`,
does nothing without them, and exits 1 only when the folder fails.

`haus import-takeout [-dry-run] [-skip-calendar Name]… <zip>…` brings
a Google Takeout archive in once (haus#13, laib ADR 0017 §4), with
Haus's own Radicale account and notes folder (`HAUS_RADICALE_*`,
`HAUS_NOTES_DIR`; one not set is left out and said so): the calendars'
entries into the Haushalt calendar, the contacts into the Haushalt
address book (both shared for now, haus#11), the Google Keep notes as
Markdown into the notes folder's `Google Notizen`, their attachments into
`Google Notizen/Anhänge`, where the memory library finds them on its next
refresh. Running it twice adds nothing twice: an entry is named after its
UID (a contact without one, Google's way, after its name and first email
or number; a note after its title and when Keep made it), and a write
never replaces what is there, so what the family changed since stays.
Trashed notes are left out, a series keeps its moved dates, each entry
carries the time zones it names. Radicale not answering or refusing ends
it; it says in German what it wrote, what was there and what not, and
exits 1 when something was not written.

## Google-Daten übernehmen (Takeout)

So kommen Kalender, Kontakte und Notizen aus einem Google-Konto einmalig
auf die Box. Termine landen im gemeinsamen Kalender „Haushalt“, Kontakte
im gemeinsamen Adressbuch „Haushalt“ (beide sehen alle, die sie auf dem
Telefon eingerichtet haben), Google Notizen als Notizen im Ordner
`notes/Google Notizen` der Dateiablage, Bilder daraus in
`notes/Google Notizen/Anhänge`.

1. **Exportieren:** Auf takeout.google.com nur *Kalender*, *Kontakte* und
   *Notizen* (Google Notizen/Keep) auswählen, Format ZIP, exportieren und
   die ZIP-Datei(en) herunterladen. Große Exporte kommen als mehrere
   Dateien (`…-001.zip`, `…-002.zip`); alle gehören zusammen.
2. **Auf die Box legen:** die Datei(en) in den Datenordner von Haus
   kopieren, das ist `<dataDir>/haus` (`dataDir` steht in der `box.yml`,
   z. B. `/mnt/data/stacks/haus`). Im Haus-Container heißt der Ordner
   `/data`.
3. **Probelauf:** zeigt nur, was übernommen würde, und schreibt nichts:

   ```
   laib exec haus --container systemd-haus -- /haus import-takeout -dry-run /data/takeout-20261015T100000Z-001.zip /data/takeout-20261015T100000Z-002.zip
   ```

   Einen Kalender, den die Telefone sowieso zeigen (etwa die Feiertage),
   lässt `-skip-calendar "Feiertage in Deutschland"` weg; mehrfach möglich.
4. **Übernehmen:** derselbe Befehl ohne `-dry-run`. Am Ende steht, wie
   viele Termine, Kontakte und Notizen neu sind, wie viele schon da waren
   und was nicht ging, mit Grund.
5. **Aufräumen:** die ZIP-Dateien aus `<dataDir>/haus` löschen. Sie
   enthalten persönliche Daten und würden sonst mitgesichert.

Gut zu wissen:
- **Ein zweiter Lauf schadet nicht.** Was schon da ist, wird nicht noch
  einmal angelegt und nicht überschrieben, auch nicht, wenn es inzwischen
  auf einem Telefon geändert wurde. Nach einem abgebrochenen Lauf einfach
  noch einmal starten.
- **Kontakte, die in Google in mehreren Gruppen stehen** („Alle Kontakte“,
  „Meine Kontakte“, eigene Labels), kommen nur einmal an. Ein Kontakt ohne
  Namen bekommt seine Nummer oder E-Mail als Namen; einer ganz ohne Namen,
  Nummer und E-Mail wird ausgelassen und genannt.
- **Notizen aus dem Papierkorb** von Google Notizen werden nicht
  übernommen; archivierte schon, mit dem Hinweis „In Keep archiviert“.
- **Die Suche** findet die übernommenen Notizen nach dem nächsten
  Auffrischen der Bibliothek, spätestens nach einer Viertelstunde.
- **Ohne Radicale-Konto** für Haus (`RADICALE_USER` und `laib secret set
  HAUS_RADICALE_PASSWORD`) werden Kalender und Kontakte ausgelassen; der
  Befehl sagt das.
- `laib exec` macht vorher eine Sicherung und zählt als Eingriff ins
  Stunden-Budget von Haus.

## Tests

`go test ./...` — a fake Laib MCP, a fake Home Assistant
(`internal/ha/hafake`, REST and WebSocket), a fake Radicale
(`internal/caldav/davfake`, calendars and address books), a fake Paperless
(`internal/paperless/paperlessfake`), a fake Vermittler in front of qmd
(`internal/qmd/qmdfake`, built from the prototype's answers on qmd
2.8.3 in `internal/qmd/testdata`), a fake model proxy
(`internal/judge/proxyfake`), a fake Bote (`internal/bote/botefake`, Hermes' API server) and a temp folder for the notes; the shapes are pinned to the app's `data/*.kt`
parsers in mdopp/haus-android. `HAUS_QMD_REAL=<a running qmd>
HAUS_QMD_REAL_NOTES=<its notes folder> go test ./internal/library -run Real`
checks against a real qmd by hand; CI never does.
No Go file names the old app or its names (`internal/app/names_test.go`).
