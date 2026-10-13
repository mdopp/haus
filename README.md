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

- `GET /box/geraete` — pairing of its own: a name in, the app's setup link
  (`cloud.dopp.solaris://setup?server=…&token=sol_device_…`) out, shown once;
  devices listed and revocable. Tokens live hashed in `HAUS_DATA/devices.json`;
  `/napi/whoami` answers for them. The scheme, the token prefix and the
  `/napi/servicebay` paths are the app's names (`internal/legacy`).

Not yet: Start, Räume, Energie, Medien, Notizen, Kalender; the `Sprechen`
field to the Bote. Those come with the Home Assistant
and Solaris deconstruction (laib#21).

## Run

```
LAIB_URL=http://host.containers.internal:5900 LAIB_TOKEN=<the haus hand's token, minted on laib add haus> \
HAUS_PROXY_SECRET=<what Caddy sends as X-Haus-Proxy> LAIB_PUBLIC_URL=https://laib.<domain> \
SOLARIS_URL=http://host.containers.internal:8787 HAUS_LISTEN=0.0.0.0:8650 ./haus
```

`HAUS_DATA` (default `/data`) holds `devices.json`; `HAUS_PUBLIC_URL` is the
origin the setup link carries (default `https://<Host>`). Haus's Laib token is the `haus` hand: status, inbox, journal, restart —
nothing else (no exec, no write). There are no static device tokens. Image: `ghcr.io/mdopp/haus`. Laib template: `haus` (route `haus.<domain>`,
`bypass: [/napi/]`).

## Tests

`go test ./...` — a fake Laib MCP and a fake Solaris; the shapes are
pinned to `SbApiClient.kt` in solaris-android.
