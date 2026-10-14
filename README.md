# Haus

**Haus** is the family's surface for the box that [Laib](https://github.com/mdopp/laib)
runs: pages for people, cards, and the companion API the Android widgets
speak. Haus renders and decides nothing; Laib acts; the Bote is who you
talk to (laib ADR 0014, 0016). Names follow laib ADR 0021: the family sees
the house persona, and the old Solaris and ServiceBay names live only in
`internal/legacy` (the installed app's contract) and the migration bridge.

## This cut (laib#28)

- `GET /box` — the page behind Authelia: readiness, the inbox read-only
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
  entity's; any `lock`, and a cover open whose `device_class` is
  garage/door/gate or cannot be read, answers **403 `sensitive_action`**
  without `confirmed:true`; `data` cannot name another target),
  `GET /napi/portal/{state,energy,entity-history,active}`, and the realtime
  pair `POST /napi/portal/watch` + `GET /napi/portal/events` (SSE: a
  `card_state` frame for each entity the device watches, `: ping` every
  90s, from one HA `state_changed` subscription while anyone listens).
  Every one wants a paired device. The shapes are `ApiClient.kt`'s; no
  JSON `null` goes out (Android reads it as `"null"`). The stream carries
  only `card_state` so far: notices and approval frames still need the
  bridge's stream.
- `GET /.well-known/assetlinks.json` — the app's signing fingerprints from
  `HAUS_ANDROID_FINGERPRINTS` (comma-separated, public), so the app opens
  Haus's pages without a URL bar. `[]` without them.

Not yet: Start, Räume, Energie, Medien, Notizen, Kalender; the `Sprechen`
field to the Bote. Those come with the Home Assistant
and Solaris deconstruction (laib#21).

## Run

```
LAIB_URL=http://host.containers.internal:5900 LAIB_TOKEN=<the haus hand's token, minted on laib add haus> \
HAUS_PROXY_SECRET=<what Caddy sends as X-Haus-Proxy> LAIB_PUBLIC_URL=https://laib.<domain> \
SOLARIS_URL=http://host.containers.internal:8787 HAUS_LISTEN=0.0.0.0:8650 \
HAUS_HA_URL=http://host.containers.internal:8123 HAUS_HA_TOKEN=<a long-lived HA token> \
HAUS_ANDROID_FINGERPRINTS=<the app's SHA-256 signing fingerprints> ./haus
```

`HAUS_DATA` (default `/data`) holds `devices.json`; `HAUS_PUBLIC_URL` is the
origin the setup link carries (default `https://<Host>`). Haus's Laib token is the `haus` hand: status, inbox, journal, restart —
nothing else (no exec, no write). There are no static device tokens. Image: `ghcr.io/mdopp/haus`. Laib template: `haus` (route `haus.<domain>`,
`bypass: [/napi/]`; assetlinks needs `/.well-known/` there too).

## Tests

`go test ./...` — a fake Laib MCP, a fake Solaris and a fake Home
Assistant (`internal/ha/hafake`, REST and WebSocket); the shapes are
pinned to `SbApiClient.kt` and `ApiClient.kt` in solaris-android.
