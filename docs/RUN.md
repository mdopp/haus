# Running Homes

On a Laib box the `homes` template does all of this; this page is for running it by hand and for knowing what each variable does. Which variable belongs to which area: [ADR 0001 §7](adr/0001-modules.md).

```
LAIB_URL=http://host.containers.internal:5900 LAIB_TOKEN=<the homes hand's token, minted on laib add homes> \
HOMES_PROXY_SECRET=<what Caddy sends as X-Homes-Proxy> LAIB_PUBLIC_URL=https://laib.<domain> \
HOMES_LISTEN=0.0.0.0:8650 HOMES_ASSISTANT=Homes \
HOMES_HA_URL=http://host.containers.internal:8123 HOMES_HA_TOKEN=<a long-lived HA token> \
HOMES_ENERGY_FLOW=pv=sensor.…,house=sensor.…,grid=sensor.…,battery=sensor.… HOMES_ENERGY_TOTALS=PV-Erzeugung=sensor.…,… \
HOMES_ENERGY_STATS=bezug=sensor.…,einspeisung=sensor.…,solar=sensor.… \
HOMES_HA_PUBLIC_URL=https://home.<domain> HOMES_COVER_INVERTED=cover.…,… HOMES_RADICALE_PUBLIC_URL=https://caldav.<domain> \
HOMES_HAUSHALT_TOKEN=<the Bote's bearer> TZ=Europe/Berlin \
HOMES_RADICALE_URL=http://host.containers.internal:5232 HOMES_RADICALE_USER=<service account> HOMES_RADICALE_PASSWORD=<its password> \
HOMES_PAPERLESS_URL=http://host.containers.internal:8000 HOMES_PAPERLESS_TOKEN=<a Paperless API token> \
HOMES_PAPERLESS_PUBLIC_URL=https://paperless.<domain> HOMES_PAPERLESS_LAN_ONLY=true HOMES_DOCUMENTS_MAIL=dokumente@<domain> HOMES_NOTES_DIR=/notes \
HOMES_QMD_URL=http://127.0.0.1:8181 HOMES_QMD_DOCUMENTS=dokumente HOMES_DOCUMENTS_DIR=/dokumente HOMES_UPLOAD_DIR=/consume \
HOMES_LLM_URL=http://host.containers.internal:11435/v1 HOMES_LLM_MODEL=gemma-4-e4b HOMES_LLM_KEY=<the model key> \
HOMES_BOTE_URL=http://host.containers.internal:8642/v1 HOMES_BOTE_KEY=<the Bote's API_SERVER_KEY> \
HOMES_STT_PROVIDER=stt.faster_whisper HOMES_STT_LANGUAGE=de \
HOMES_SAMBA_DIR=/samba-accounts HOMES_SAMBA_HOST=<the box's LAN address> \
HOMES_ARCHIV_DIR=/archiv-eingang \
HOMES_REITTI_URL=http://host.containers.internal:<Reitti's host port> HOMES_REITTI_PUBLIC_URL=https://reitti.<domain> \
HOMES_IMMICH_URL=http://host.containers.internal:2283 HOMES_IMMICH_KEY=<an API key of the owner's Immich account: person.read, person.create, person.update, user.read> HOMES_IMMICH_PUBLIC_URL=https://photos.<domain> \

HOMES_USAGE_URL=http://host.containers.internal:8094 HOMES_USAGE_TOKEN=<usage-metrics' USAGE_METRICS_TOKEN> ./homes
```

`HOMES_DATA` (default `/data`) holds `devices.json` and `favoriten.json`; `HOMES_PUBLIC_URL` is the
origin the setup link carries (default `https://<Host>`). Homes' Laib token is the `homes` hand: status, inbox, journal, restart —
nothing else (no exec, no write). There are no static device tokens. Image: `ghcr.io/mdopp/homes`. Laib template: `homes` (routes `homes.<domain>` and,
until laib#1021 ends, the old `haus.<domain>`, both `bypass: [/napi/]`; the
upload needs the share's consume folder mounted writable at
`HOMES_UPLOAD_DIR`, on a file system with hard links, and Caddy letting a
request of 64 MB through; the message backups need
`${DATA_DIR}/archiv-eingang` mounted writable at `HOMES_ARCHIV_DIR`,
`/archiv-eingang`, and a 16 MB request body).

`homes export-documents` runs once instead of the server and exits (the
refresh timer runs it in Homes' container, `podman exec systemd-homes
/homes export-documents`, before `qmd update`): it reads
`HOMES_PAPERLESS_URL`, `HOMES_PAPERLESS_TOKEN` and `HOMES_DOCUMENTS_DIR`,
does nothing without them, and exits 1 only when the folder fails.

`homes import-takeout [-dry-run] [-skip-calendar Name]… <zip>…` brings
a Google Takeout archive in once (haus#13, laib ADR 0017 §4), with
Homes' own Radicale account and notes folder (`HOMES_RADICALE_*`,
`HOMES_NOTES_DIR`; one not set is left out and said so): the calendars'
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

