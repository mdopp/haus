# Homes — working notes for assistants

Homes is one of Laib's three programs (laib `docs/adr/0014-…`): the surface.
Read `mdopp/laib` `docs/KONZEPT.md`, `docs/DEKONSTRUKTION.md` (Homes: the pages
and the cards) and `docs/adr/README.md` before changing shape. The app's
contract is the Homes app in `mdopp/homes-android` (laib ADR 0022): its
`data/*.kt` parsers' field names are the law. Its names (deep-link scheme,
token prefix, `/napi/box`) live in `internal/app`, one set; nothing old is
carried (ADR 0022 §4) except what the rename from Haus keeps until every phone
runs Homes (laib#1021): the `haus_device_` tokens, the `haus.<domain>`
address for `/napi/`, `GET /napi/app`. Names stored in other apps keep the
old name (the iCal `X-HAUS-*` and PRODIDs, usage-metrics' app `haus`); each
is marked where it is defined. The program's own logins are `homes` (owner,
2026-11-03): a collection path Homes stored under `/haus/` is read as
`/<HOMES_RADICALE_USER>/` (`caldav.LegacyAccountPath`, ends laib#1021 step 9).

Rules: no framework, one page per file, every Laib call is one MCP tool call
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

**Gates that keep the code clean (owner, 2026-11-03: „Code sollte immer
möglichst sauber sein“).** Both run in CI; run them before a PR.

- `scripts/loc.sh` — production lines per area under its cap (pages,
  napi, cmd, connectors, household, shared; set at the size of
  2026-11-03 plus 5 %, the reason next to each number), every
  `internal/` package in exactly one area, and who may import whom: a
  connector (a client of one other service) and a shared package import
  no Homes package; only `cmd/` imports pages; only `cmd/` and pages
  import napi; a fake only the tests and `cmd/homesdemo`. Crossing a cap
  means raising it in the same PR with its reason and the owner's yes.
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
