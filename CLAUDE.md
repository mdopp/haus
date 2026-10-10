# Haus — working notes for assistants

Haus is one of Laib's three programs (laib `docs/adr/0014-…`): the surface.
Read `mdopp/laib` `docs/KONZEPT.md`, `docs/DEKONSTRUKTION.md` (Haus: the pages
and the cards) and `docs/adr/README.md` before changing shape. The companion
contract is `servicebay/docs/COMPANION_APP.md` plus `solaris-android`
`data/SbApiClient.kt`: the app's field names are the law.

Rules: no framework, one page per file, every Laib call is one MCP tool call
(`internal/laib`), Haus never decides — a verdict is the admin's token passed
on. Tests use fakes for Laib and Solaris; never a live box.
