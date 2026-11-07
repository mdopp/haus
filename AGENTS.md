# Homes — notes for coding agents

Everything a coding agent (Claude Code, pi, anyone else) needs to change
Homes: the rules, the gates, how to test and ship, and the traps. Humans
start at the [README](README.md); the full page-by-page reference is
[`docs/REFERENCE.md`](docs/REFERENCE.md).

## What Homes is, and where its decisions live

Homes is one of Laib's three programs (laib `docs/adr/0014-…`): the surface.
Read `mdopp/laib` `docs/KONZEPT.md`, `docs/DEKONSTRUKTION.md` (Homes: the pages
and the cards) and `docs/adr/README.md` before changing shape. The app's
contract is the Homes app in `mdopp/homes-android` (laib ADR 0022): its
`data/*.kt` parsers' field names are the law. Its names (deep-link scheme,
token prefix, `/napi/box`) live in `internal/app`, one set; nothing old is
carried (ADR 0022 §4) except what the rename from Haus keeps until every phone
runs Homes (laib#1021): the `haus_device_` tokens and `GET /napi/app`
(the address `haus.<domain>` is gone since 2026-11-07: Homes is
`www.<domain>`, `HOMES_PUBLIC_URL`, and no host is written in the code). Names stored in other apps keep the
old name (the iCal `X-HAUS-*` and PRODIDs, usage-metrics' app `haus`); each
is marked where it is defined. The program's own logins are `homes` (owner,
2026-11-03): a collection path Homes stored under `/haus/` is read as
`/<HOMES_RADICALE_USER>/` (`caldav.LegacyAccountPath`, ends laib#1021 step 9).

Homes' own decisions are in `docs/adr/` (Laib's stay in laib's): read
ADR 0001 (modules: the list of areas, `GET /napi/kann`, `HOMES_MODULE_AUS`)
before adding an area, a page, a `/napi` route or a `HOMES_*` variable.

## Rules

No framework, one page per file, every Laib call is one MCP tool call
(`internal/laib`), Homes never decides — there is no verdict in Homes at all (haus#1): the
Box page links to Laib's inbox, `/napi/…/approve|deny` answer 403, and Homes
holds only the `homes` hand's token. Tests use fakes for Laib, Home Assistant,
Radicale and Paperless; never a live box.

**UX is part of the feature (owner, 2026-10-23; laib CLAUDE.md rule 4).**
Homes is what the family touches, so it carries this rule first: every tap
answers at once (pressed state, "läuft …" on the button itself), long work
shows its step and progress, and every action ends visibly where it was
started — done, failed with the reason in plain German and the next step,
or waiting with the choices. When Laib answers asynchronously ("Auftrag
angenommen"), Homes polls the status and shows the result; it never drops
it. No silent buttons, no endless spinners (a timeout becomes an honest
message), no raw errors, phone layout first. Tests cover the states: idle,
running, done, failed, timed out. A page or button without them is not
finished.

**The assistant's name appears where you talk to it.** One name for the
whole box, set once in Laib (box.yml `assistant:` → `HOMES_ASSISTANT`,
*Homes* by default; `internal/assistent`). Family pages wear the house's
palette; the name appears where a sentence says how to address it („Sag
Homes: Merk dir …“, owner 2026-10-24). Every speaker wakes on „Hey
Mycroft“; nothing trains a wake word.

**Scripts are welcome (owner, 2026-10-24).** Use them wherever they make
Homes better to use — the pressed button's "Speichert …" (`pressScript`),
a part fetched after the page, a live state, recording. A page need not
work with script off. Homes' scripts are its own and inline: never one
loaded from elsewhere (privacy, and the box stays self-contained), no
framework, no build step.

## Code gates

The owner, 2026-11-03: „Code sollte immer möglichst sauber sein“. Both
gates run in CI; run them before a PR.

- `scripts/loc.sh` — production lines per area under its cap (pages,
  napi, cmd, connectors, household, shared; set at the size of
  2026-11-03 plus 5 %, the reason next to each number), every
  `internal/` package in exactly one area, and who may import whom: a
  connector (a client of one other service) imports only shared packages,
  a shared package imports no Homes package; only `cmd/` imports pages; only `cmd/` and pages
  import napi; a fake only the tests and `cmd/homesdemo`. Crossing a cap
  means raising it in the same PR with its reason and the owner's yes.
  The text catalog (`internal/texte/katalog.go`, the family's words and
  their translations) is not counted: words, not code (owner 2026-11-04).
  CI also fails on a file gofmt would change (lines count as gofmt
  writes them).
- `scripts/ratchet.py` — deadcode (`dead`: no caller even in a test;
  `test-only`: production code only a test runs, fakes aside),
  staticcheck (defaults + U1000, not ST1005: the error strings are German
  sentences), dupl (75 tokens, production) and gocognit (over 30,
  production), pinned, against today's findings in `scripts/ratchet/`.
  A new or higher finding fails (a complexity score counts): fix it, or
  if it must stay `scripts/ratchet.py --update --accept <tool>` with the
  reason in the PR and the owner's yes. A finding that went away fails
  too until the same PR lowers the baseline: `scripts/ratchet.py
  --update` (it only lowers) and commit `scripts/ratchet/`. A renamed or
  moved function counts as new under its new name. The script is the
  same as laib's `scripts/ratchet.py`; change both.


## Layout

```
cmd/homes/        the binary: env → wiring → routes; also `homes export-documents`
                  and `homes import-takeout` (docs/RUN.md)
cmd/homesdemo/    the family pages against the fakes, for e2e/ and for looking
internal/pages/   the family's pages, one per file (+ the inline script, skript.go)
internal/napi/    /napi, the app's API (docs/REFERENCE.md; the app's side:
                  homes-android docs/CONTRACT.md)
internal/<client> one package per other service (ha, caldav, paperless, reitti,
                  jellyfin, abs, bote, laib, qmd, …), each with its *fake
internal/<thing>  the household's own logic (einkauf, energie, favoriten,
                  notes, podcasts, schalter, takeout, usage, …)
docs/adr/         Homes' own decisions (ADR 0001: modules)
docs/             REFERENCE (pages, /napi), RUN (env, commands), USAGE (the
                  counts), TAKEOUT (Google import, German), img/ (README pictures)
e2e/              Playwright against cmd/homesdemo
scripts/          loc.sh (caps, import rules), ratchet.py (+ ratchet/ baselines),
                  readme_langs.py (the four READMEs)
tools/icons/      renders the web icons from internal/pages/symbol/icon.svg
```

Which `scripts/loc.sh` area a package is in (connectors, shared, household,
pages, napi, cmd) is listed at the top of that script; a new package names
its area there.

## Build, test, run

```
gofmt -l .                      # must print nothing
go vet ./... && go test ./...
scripts/loc.sh                  # caps and import rules
scripts/ratchet.py              # dead code, staticcheck, dupl, gocognit
go run ./cmd/homesdemo          # look at the pages on fakes
cd e2e && npm ci && npx playwright install chromium && npx playwright test
```

CI (`.github/workflows/ci.yml`) runs `test` (gofmt, vet, tests, build,
loc.sh), `ratchet` and `e2e` on every PR. Running Homes against real
backends: [`docs/RUN.md`](docs/RUN.md).

## Tests

`go test ./...` — a fake Laib MCP, a fake Home Assistant
(`internal/ha/hafake`, REST and WebSocket), a fake Radicale
(`internal/caldav/davfake`, calendars and address books), a fake Paperless
(`internal/paperless/paperlessfake`), a fake Vermittler in front of qmd
(`internal/qmd/qmdfake`, built from the prototype's answers on qmd
2.8.3 in `internal/qmd/testdata`), a fake model proxy
(`internal/judge/proxyfake`), a fake Bote (`internal/bote/botefake`, Hermes' API server) and a temp folder for the notes; the shapes are pinned to the app's `data/*.kt`
parsers in mdopp/homes-android. `HOMES_QMD_REAL=<a running qmd>
HOMES_QMD_REAL_NOTES=<its notes folder> go test ./internal/library -run Real`
checks against a real qmd by hand; CI never does.
No Go file names the old app or its names (`internal/app/names_test.go`).

**In a browser** (`e2e/`, CI job `e2e`): `cmd/homesdemo` serves the family
pages against the fakes (lamps, a cover, a player that change when
switched, a notes folder; everyone is anna, `/demo/fehler?status=503`
makes switching fail, `/demo/setze?id=…&state=…` changes a state from
elsewhere). Playwright drives it on a phone-sized viewport: a switch flips
at once and the page stays (no reload), a refusal is said on the page,
the page follows a change made elsewhere within seconds (push), a player pauses in place, a
note is written and changed, and no page throws a script error. Pictures
of the main pages are kept with each run. Locally: `cd e2e && npm ci &&
npx playwright install chromium && npx playwright test`; `go run
./cmd/homesdemo` alone to look at the pages.


## Shipping

- A merge to `main` builds `ghcr.io/mdopp/homes:latest` (and `:<sha>`) once
  `test` and `e2e` are green. There is no release step in this repo.
- The box runs `:latest` through Laib's `homes` template
  (`mdopp/laib` `templates/homes`). Laib's nightly image update brings it
  there, verified, with the previous image back on red; `laib updates
  --now` starts that run at once. Homes is never restarted or changed on
  the box by hand from here: Laib changes the box, you propose.
- Screenshots for the README come from the `e2e` job's artifact
  (`gh run download <run> -n e2e`): demo data only (everyone is `anna`),
  under `docs/img/`, WebP, a few hundred KB at most.

## Pitfalls

- **The app's parsers are the contract.** A field renamed in `/napi` breaks
  phones in the field. homes-android's `NapiContractTest` pins a copy of the
  handlers; change both sides, the server first, additively.
- **Old names stay where data was stored under them** (`haus_device_`
  tokens, the iCal `X-HAUS-*` properties, usage-metrics'
  app `haus`). They are marked where defined; do not "clean them up".
- **No secret in the repo**, not even in a test: tokens reach Homes as
  `HOMES_*` from Laib's secrets.
- **The pages are German, for the family; the code, commits and docs are
  English.** A plain German sentence is the error a person sees, never
  the raw error.
- **A green CI is not a verified feature.** On the box, look at the page
  (or have the owner look) before you say it works.
