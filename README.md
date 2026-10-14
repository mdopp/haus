# Haus

**Haus** is the family's surface for the box that [Laib](https://github.com/mdopp/laib)
runs: pages for people, cards, and the companion API the Android widgets
speak. Haus renders and decides nothing; Laib acts; the Bote is who you
talk to (laib ADR 0014, 0016). Names follow laib ADR 0021: the family sees
the house persona, and the old Solaris and ServiceBay names live only in
`internal/legacy` (the installed app's contract) and the migration bridge.

## This cut (laib#28)

- `GET /start` — the family's page behind Authelia (haus#12): the
  persona's header, the three guarantees, then the cards *Jetzt* (who is
  home from `person.*`, the lamps that are on with their names (a group,
  any entity with an `entity_id` list, is not counted), what
  plays on a `media_player`, the next appointment within 14 days) and
  *Haushalt* (the next three open reminders, the three newest notes).
  Each part only when its backend is set (`HAUS_HA_URL` + `HAUS_HA_TOKEN`,
  `HAUS_RADICALE_*`, `HAUS_NOTES_DIR`); one not set, or not answering
  within 4s, is left out without a word, its error in Haus's log. The
  page shows and changes nothing (haus#1). Cards are one renderer,
  `internal/cards` (title, rows, optional link), for the pages and later
  the widgets' JSON. `/` and every page path Haus does not serve redirect
  to `/start`: the app's hash routes (`/#/p/start`, `/#/p/energy`,
  `/#/p/device/<id>`, `/#/p/camera[/<id>]`,
  `/#/p/servicebay/approvals/<id>`, `/#/p/<tool>/item/<id>`,
  `/#/?tool=<id>`, `/#/?ask=<text>`) arrive as `/` and all land on Start
  until their pages exist; so does `/download`.
  Each card row and card links to the page behind it: *Jetzt* to Räume,
  an appointment or reminder to Kalender, a note to its own page.
- `GET /raeume`, `/kalender`, `/notizen` — the family's other pages
  (haus#12), read-only, from the same cards, under the persona's header and
  one nav (Start · Räume · Kalender · Notizen, each only when its backend is
  set; Box for admins). A page whose backend is not set lands on Start; one
  not answering within 4s is left out, its error in the log. Anything but
  GET/HEAD is 405.
  - *Räume*: one card per Home Assistant area (the entity's own, else its
    device's), rooms by name: lights (on/off, brightness), heating (current
    and target temperature), covers (open/closed, position), media (what
    plays); groups left out, at most 40 rows a room; entities without an
    area in *Ohne Raum* at the end. The rooms are read over HA's WebSocket
    and kept 5 minutes (shared with `/napi/portal/active`); until a read
    has worked, the cards are left out rather than put everything in Ohne
    Raum. Switching comes later, with where actions live.
  - *Kalender*: *Heute* (the day's appointments with time and place),
    *Nächste 14 Tage*, *Erinnerungen* (open reminders, overdue said so),
    every calendar of the Radicale account, at most 50 rows a card.
  - *Notizen*: a search field (`GET /notizen?q=…`, the notes search above:
    20 hits, its caps) and the ten newest notes. `/notizen/<file>` (the
    note's file name, e.g. `einkaufsliste.md`; a slug or title works too)
    shows the note as text: paragraphs and line breaks, no Markdown
    rendering, never markup from the file. Outside the folder, hidden or
    missing is a 404.
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
- `/napi/servicebay/{home,services,approvals,upgrades}` — the four keys the
  widgets read, from `laib status`, `laib inbox`. `POST …/services/{name}/operate`
  (restart). `POST …/approvals/{id}/approve|deny` answers 403: Haus decides
  nothing (haus#1); phones decide, if ever, through the Signal relay.
- The migration bridge (`internal/napi/bridge.go`, set by `SOLARIS_URL`):
  every other `/napi/*` path is passed through to the retired Solaris, and
  a device token is also valid when Solaris says so (`GET /napi/whoami`),
  so a phone paired there keeps working. It ends when every phone is paired
  through Haus and Haus serves every path the app uses (haus#5).

- Pairing: in the app, enter `https://haus.<domain>`. The app opens
  `/pair-device` in a Custom Tab, Authelia logs the person in, they confirm,
  and Haus redirects to `<scheme>://pair#token=…&id=…`, the token in the
  fragment (haus#6). `GET /box/geraete` lists the devices, revocable, and can
  mint a token whose setup URI (`<scheme>://setup?server=…&token=…`) goes into
  a QR code the app scans. That URI is QR content, not a link: the app only
  opens `…://pair`. Tokens live hashed in `HAUS_DATA/devices.json`;
  `/napi/whoami` answers for them. The scheme, the token prefix and the
  `/napi/servicebay` paths are the app's names (`internal/legacy`).

- The house persona (laib ADR 0020): Laib mounts `render/persona` at
  `/persona` and sets `HAUS_PERSONA`. Pages carry its name in the title,
  a header with its icon, name and greeting in its colours, and
  `/persona/icon.svg`. The app reads `GET /napi/persona` (display, greeting,
  accent, ground, icon) and `/napi/persona/icon.svg` with its device token.
  Without a persona Haus is plain Haus; internally it is always `haus`.

- The house paths, from Home Assistant (haus#10, set by `HAUS_HA_URL` +
  `HAUS_HA_TOKEN`, a long-lived token; unset, they stay on the bridge):
  `POST /napi/ha/call` (`{entity_id, service, data?, confirmed?}`; only
  `light|switch|cover|climate|lock`, else 400; the service's domain is the
  entity's; `data` cannot name another target; any `lock`, and on a
  garage/door/gate cover — or one whose `device_class` cannot be read —
  anything but `close_cover`, `stop_cover`, `close_cover_tilt`,
  `stop_cover_tilt`, answers **403 `sensitive_action`** without
  `confirmed:true`; HA refusing the call is 400, HA in trouble is 502
  `ha_unavailable`, HA's detail stays in Haus's log) and
  `GET /napi/portal/{state,energy,entity-history,active}`. Every one wants
  a paired device. The shapes are `ApiClient.kt`'s; no JSON `null` goes out
  (Android reads it as `"null"`).
- The energy picture reads the entities `HAUS_ENERGY_FLOW`
  (`pv=sensor.…,house=sensor.…,grid=sensor.…,battery=sensor.…`, any subset)
  and `HAUS_ENERGY_TOTALS` (`<Label>=sensor.…,…`, in that order) name;
  every other power sensor is a circuit. Unset, it falls back to matching
  one house's German friendly names (`Aktuell erzeugter PV-Strom`, …).
- The realtime pair `POST /napi/portal/watch` + `GET /napi/portal/events`
  (SSE: a `card_state` frame for each entity the device watches, the
  current cards again after every (re)subscribe, a sensor at most every
  10s, `: ping` every 90s, at most three streams per device; watch answers
  503 while HA does not) is Haus's **only when no bridge is set**: the app
  holds one stream, and notices and approval frames still come from the
  bridge's.
- `GET /.well-known/assetlinks.json` — the app's signing fingerprints from
  `HAUS_ANDROID_FINGERPRINTS` (comma-separated, public), so the app opens
  Haus's pages without a URL bar. `[]` without them.

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
    `kontakt_anlegen(name, telefon?, email?, geburtstag?)` (a vCard 4.0
    with a random `urn:uuid` UID; `geburtstag` `JJJJ-MM-TT` or `--MM-TT`).
    Reads cover every address book of the account; writes go to
    `HAUS_RADICALE_ADDRESSBOOK` when set, else the address book named
    `Haushalt`, made on the first write at `/<user>/haushalt-kontakte/`.
    A read stops at 5000 cards, 64 MB per address book or the 15 s
    timeout (after the first card) and says so. No redirect is followed.
  - `HAUS_PAPERLESS_URL` + `HAUS_PAPERLESS_TOKEN` (a Paperless API token;
    what its user may see is what the Bote sees): `dokumente_suchen(text)`,
    full text, at most 20 hits with date, correspondent, title and, when
    `HAUS_PAPERLESS_PUBLIC_URL` is set, a link to
    `<public>/documents/<id>/details`. Read-only (GET only), no redirect
    followed, the token never in an answer.
  - `HAUS_NOTES_DIR` (the share's `notes` folder): `notizen_suchen(text)`,
    `notiz_lesen(titel)`, `notiz_anlegen(titel, text)`. A note is
    `<slug>.md`, `# Titel` first, never written over an existing one (`-2`,
    `-3`; a share without hard links is an error); no name reaches outside
    the folder, hidden files and folders are never notes, a search reads at
    most 5000 files and 64 MB and says when it stopped early.
  Times are local (`TZ`, e.g. `Europe/Berlin`): `JJJJ-MM-TT` or
  `JJJJ-MM-TTTHH:MM`. Haus keeps nothing of it (ADR 0017 §3): a note is a
  file the family syncs, a reminder an entry on their phones; a search
  reads the files, there is no second index. Identity per person (the
  person's own calendar, laib ADR 0017 §3) comes next.

Not yet: Start's *Meine Favoriten* and *Häufig genutzt*; switching in
Räume; Energie, Medien; Paperless and dictation in Notizen, timers in
Kalender; the `Sprechen` field to the Bote. Those come with the Home Assistant
and Solaris deconstruction (laib#21).

- The Box page's Werkstatt card (haus#15): the open tickets of the repos in
  `HAUS_WERKSTATT_REPOS` (owner/name, comma-separated), what pi works
  (`pi:ready`) and what waits for Claude (`needs-claude`), read-only and
  linked. Public GitHub API, no token, cached for 5 minutes. Unset: no card.

## Run

```
LAIB_URL=http://host.containers.internal:5900 LAIB_TOKEN=<the haus hand's token, minted on laib add haus> \
HAUS_PROXY_SECRET=<what Caddy sends as X-Haus-Proxy> LAIB_PUBLIC_URL=https://laib.<domain> \
SOLARIS_URL=http://host.containers.internal:8787 HAUS_LISTEN=0.0.0.0:8650 \
HAUS_HA_URL=http://host.containers.internal:8123 HAUS_HA_TOKEN=<a long-lived HA token> \
HAUS_ANDROID_FINGERPRINTS=<the app's SHA-256 signing fingerprints> \
HAUS_ENERGY_FLOW=pv=sensor.…,house=sensor.…,grid=sensor.…,battery=sensor.… HAUS_ENERGY_TOTALS=PV-Erzeugung=sensor.…,… \
HAUS_HAUSHALT_TOKEN=<the Bote's bearer> TZ=Europe/Berlin \
HAUS_RADICALE_URL=http://host.containers.internal:5232 HAUS_RADICALE_USER=<service account> HAUS_RADICALE_PASSWORD=<its password> \
HAUS_PAPERLESS_URL=http://host.containers.internal:8000 HAUS_PAPERLESS_TOKEN=<a Paperless API token> \
HAUS_PAPERLESS_PUBLIC_URL=https://paperless.<domain> HAUS_NOTES_DIR=/notes ./haus
```

`HAUS_DATA` (default `/data`) holds `devices.json`; `HAUS_PUBLIC_URL` is the
origin the setup link carries (default `https://<Host>`). Haus's Laib token is the `haus` hand: status, inbox, journal, restart —
nothing else (no exec, no write). There are no static device tokens. Image: `ghcr.io/mdopp/haus`. Laib template: `haus` (route `haus.<domain>`,
`bypass: [/napi/]`; assetlinks needs `/.well-known/` there too).

## Tests

`go test ./...` — a fake Laib MCP, a fake Solaris, a fake Home
Assistant (`internal/ha/hafake`, REST and WebSocket), a fake Radicale
(`internal/caldav/davfake`, calendars and address books), a fake Paperless
(`internal/paperless/paperlessfake`) and a temp folder for the notes; the shapes are
pinned to `SbApiClient.kt` and `ApiClient.kt` in solaris-android.
