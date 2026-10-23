# Haus — working notes for assistants

Haus is one of Laib's three programs (laib `docs/adr/0014-…`): the surface.
Read `mdopp/laib` `docs/KONZEPT.md`, `docs/DEKONSTRUKTION.md` (Haus: the pages
and the cards) and `docs/adr/README.md` before changing shape. The app's
contract is the Haus app in `mdopp/haus-android` (laib ADR 0022): its
`data/*.kt` parsers' field names are the law. Its names (deep-link scheme,
token prefix, `/napi/box`) live in `internal/app`, one set; nothing old is
carried (ADR 0022 §4).

Rules: no framework, one page per file, every Laib call is one MCP tool call
(`internal/laib`), Haus never decides — there is no verdict in Haus at all (haus#1): the
Box page links to Laib's inbox, `/napi/…/approve|deny` answer 403, and Haus
holds only the `haus` hand's token. Tests use fakes for Laib, Home Assistant,
Radicale and Paperless; never a live box.

**UX is part of the feature (owner, 2026-10-23; laib CLAUDE.md rule 4).**
Haus is what the family touches, so it carries this rule first: every tap
answers at once (pressed state, "läuft …" on the button itself), long work
shows its step and progress, and every action ends visibly where it was
started — done, failed with the reason in plain German and the next step,
or waiting with the choices. When Laib answers asynchronously ("Auftrag
angenommen"), Haus polls the status and shows the result; it never drops
it. No silent buttons, no endless spinners (a timeout becomes an honest
message), no raw errors, phone layout first. Tests cover the states: idle,
running, done, failed, timed out. A page or button without them is not
finished.
