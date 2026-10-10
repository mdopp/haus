# Haus

**Haus** is the family's surface for the box that [Laib](https://github.com/mdopp/laib)
runs: pages for people, cards, and the companion API the Android widgets
speak. Laib ADR 0014: the Operator, Solaris and Haus are three programs;
Haus renders, Laib acts, Solaris listens.

## This cut (laib#28)

- `GET /box` — the admins' page behind Authelia: readiness, the inbox with
  Freigeben/Ablehnen (delivered to Laib with the admin's token), what runs,
  the journal. Haus listens on loopback; only Caddy reaches it.
- `/napi/servicebay/{home,services,approvals,upgrades}` — the four keys the
  widgets read, from `laib status`, `laib inbox`. `POST …/services/{name}/operate`
  (restart) and `POST …/approvals/{id}/approve|deny`.
- Every other `/napi/*` path is passed through to Solaris (`SOLARIS_URL`),
  and a device token is valid when Solaris says so (`GET /napi/whoami`).
  So the phone changes one hostname and keeps its pairing.

Not yet: Start, Räume, Energie, Medien, Notizen, Kalender; pairing of its
own; the `Sprechen` field to the Bote. Those come with the Home Assistant
and Solaris deconstruction (laib#21).

## Run

```
LAIB_URL=http://host.containers.internal:5900 LAIB_TOKEN=<laib token mint ops --note haus> \
LAIB_HUMAN_TOKEN=<laib token mint human --note haus-admin> SOLARIS_URL=http://host.containers.internal:8787 \
HAUS_LISTEN=127.0.0.1:8650 ./haus
```

`HAUS_DEVICE_TOKENS=a,b` accepts fixed device tokens (a trial box without
Solaris). Image: `ghcr.io/mdopp/haus`. Laib template: `haus` (route `haus.<domain>`,
`bypass: [/napi/]`).

## Tests

`go test ./...` — a fake Laib MCP and a fake Solaris; the shapes are
pinned to `SbApiClient.kt` in solaris-android.
