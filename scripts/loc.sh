#!/usr/bin/env sh
# One budget per area of Homes, as laib's scripts/loc.sh has one per program
# (owner, 2026-11-03: "Code sollte immer möglichst sauber sein"). Production
# lines only (not *_test.go), counted as gofmt writes them; tests are not
# capped. Each cap was set at the area's size that day plus 5 %.
#
# Crossing a cap means raising it in the same PR, with its reason next to the
# number and the owner's approval; at 98 % this warns (95 % would warn from
# the first day, the caps being 5 % above it). Every package under
# internal/ is in exactly one area: a new package names its area here.
#
# The areas, and who may import whom (production imports, `go list`):
# - connectors: a client of one other service. It imports only shared
#   packages: it knows its service, nothing of the household.
# - shared: the small shapes everyone uses (the card, Markdown, the app's
#   names, the guarded web fetch, a name compared). It imports no Homes
#   package.
# - household: the household's own logic, on top of connectors and shared.
# - napi: the app's API; pages: the family's pages; cmd: the wiring.
#   Only cmd imports pages; only cmd and pages import napi.
# - A fake (a package whose directory ends in "fake") is for the tests and
#   cmd/homesdemo only.
set -e
cd "$(dirname "$0")/.."
bad=0
p() { for x in "$@"; do printf 'internal/%s ' "$x"; done; }
connectors=$(p abs bote caldav discogs ha immich jellyfin judge laib musicbrainz paperless qmd reitti samba werkstatt)
shared=$(p abruf app assistent cards code128 jsonfile markdown mcpserver modul namen texte)
household=$(p ansicht devices dokumente einkauf einrichten energie favoriten gesehen gesichter haushalt library merken musik notes papierkorb podcasts schalter takeout usage)

budget() { # budget <name> <cap> <dir…>
  name=$1 cap=$2; shift 2
  prod=$(find "$@" -name '*.go' ! -name '*_test.go' -exec cat {} + 2>/dev/null | wc -l)
  test=$(find "$@" -name '*_test.go' -exec cat {} + 2>/dev/null | wc -l)
  echo "$name production: $prod  tests: $test  (cap $cap)"
  [ "$prod" -lt "$cap" ] || { echo "$name over $cap lines"; bad=1; }
  [ "$((prod * 100))" -lt "$((cap * 98))" ] || [ "$prod" -ge "$cap" ] || echo "warning: $name at $prod of $cap lines, 98 % or more"
}
# area       cap    dirs              reason (2026-11-03: the size that day, plus 5 %)
# 2026-11-05, owner 2026-11-05: contact jump links (15,415 -> 15,626): /kontakte/<uid>/dokumente and
# /nachrichten to Paperless and the viewer's archive, the contact page's buttons, the phone book's URLs
# 2026-11-05, owner 2026-11-05: So hängt alles zusammen (15,626 -> 15,740): the family's page under
# Mehr, the web of what links their data and the „Das geht“ moments, each shown while its modules are on
# 2026-11-05, owner 2026-11-05 yes: Immich for the contacts (15,740 -> 15,949): the
# „Fotos“ link and the face in the phones' cards (kontakt_sprung.go), the suggestions on Kontakte with anlegen
# and ausblenden (kontakte_immich.go), Fotos in „So hängt alles zusammen“
# 2026-11-06, owner approved 2026-11-06: Box → Auslastung (15,949 -> 16,090): the tab, its bars,
# rings and temperatures, its values fetched again in place every 5 s (box_auslastung.go, laib#1076)
# 2026-11-06, owner approved 2026-11-06: Dokumente-Suche (16,089 -> 16,371): Finden on Dokumente and /dokumente/suche (dokumente_suche.go): the search field fetched
# while typing, the Bereich tiles, type and person chips, Versicherungen and Garantie läuft noch, the rows
# 2026-11-06, owner approved 2026-11-06: Dieselbe Person: „Dieselbe Person?“ (16,341 -> 16,497):
# /kontakte/dieselbe (kontakte_dieselbe.go): pairs of faces, ten a page, Ja/Nein/Überspringen, the outcome on the page, the quiet links
# 2026-11-06, owner approved 2026-11-06: Kontaktfotos: „Ist das …?“ (16,497 -> 16,555):
# a contact's picture beside the person Immich gave its face, Ja/Nein/Kein Foto (kontakte_ist.go), on „Dieselbe Person?“
# 2026-11-07, owner approved 2026-11-07: Befunde in der Box-Übersicht (16,555 -> 16,574): laib befunde
# (laib#1085) instead of diag, „Befunde gemessen vor X min“ (seit), the line before diag's first run
# 2026-11-07, PENDING the owner's yes: Dieselbe Person, Kontext und Aufgabe (16,574 -> 16,579): each face a
# link into Immich, a grey line for its places and its company, the questions as one task on Deine Aufgaben
budget pages 16580 internal/pages     # the family's pages, one per file: 14,684
# 2026-11-05, owner 2026-11-05: writable Homes-Konto (5,033 -> 5,121): PUT and DELETE of one shared
# appointment or card for the phones, the Papierkorb on delete, the feed for a phone that writes back
budget napi 5121 internal/napi        # the app's widgets' and share sheet's API: 4,793
# 2026-11-04, homes ADR 0001 step H1 (owner: „ganz durchziehen“): the module catalog,
# cmd/homes/module.go, 1,150 with it; plus 5 %. H3–H6 move the routes out of main.go into it
# 2026-11-04, step H8 (§10, owner: „Keine Funktion soll versteckt werden“): the routes of the traces
# and closing a wish once its module is on, 1,209; plus 5 %
# 2026-11-04, step H3: HOMES_MODULE_AUS switches the leaf modules off, 1,298; plus 5 %
# 2026-11-04, steps H4–H6: HOMES_MODULE_AUS switches every module off (the switches, the
# collections per module, the background work after the list), 1,379; plus 5 %
# 2026-11-04, the owner (the caps count production code only): cmd/homesdemo is the demo on
# fakes for the browser tests and the pictures, never run on a box; it is not counted. The
# cap stays as it was, now for cmd/homes alone
budget cmd 1448 cmd/homes             # homes' wiring: 1,018 with the demo then
# 2026-11-04, the caps moved with the packages, none raised (sum 24,134 as before): abs and
# musicbrainz (1,227 lines, 1,289 of cap) from household to connectors once merken's fetcher
# was internal/abruf (235 lines of merken, 247 of cap, to shared) and jellyfin.Plain
# internal/namen (11 lines, 12 of cap, to shared)
# 2026-11-05, owner 2026-11-05: writable Homes-Konto (13,171 -> 13,467): the logic of that napi raise
# lives in caldav (schreiben.go: find by UID, write over If-Match keeping what the phone does not show,
# delete, put back; the feed of whole series with their ETags), so this part of the budget moved with it
# 2026-11-05, owner 2026-11-05 yes: Immich for the contacts (13,466 -> 13,688):
# internal/immich, the people and their faces with the owner's key, and its fake (immichfake); caldav takes a
# picture the phone sends (sentOnly)
# 2026-11-06, owner approved 2026-11-06: Box → Auslastung (13,670 -> 13,714): laib.Auslastung,
# the shape of laib auslastung's JSON and its call (laib#1076)
# 2026-11-06, owner approved 2026-11-06: Dokumente-Suche (13,714 -> 14,098): paperless.Finde and Ordnung
# (suche.go: Bereiche, types, Person; the number fields by custom_field_query, sender, text, merged and kept a
# minute; Vertraege shares its reading, -64), and paperlessfake's custom_field_query (AND/OR/NOT, icontains, gt,
# exists), text, a sender's name, document counts and a select's options (+95)
# 2026-11-06, owner approved 2026-11-06: Kontakte → Immich, every contact a person in Immich (14,098 -> 14,288): internal/immich
# searches people by name (faceless and hidden too), makes one, sets its name and birthday, says whose
# key it is and which permission it lacks (107 -> 190); immichfake answers all of it, keeping a faceless
# person out of its list as Immich does, with the key's permissions (99 -> 206)
# 2026-11-06, owner approved 2026-11-06: Dieselbe Person: Immich merges (14,288 -> 14,434):
# immich.Zusammenfuehren (POST /api/people/merge), Naechste (closestPersonId), Fotos (statistics), Rechte (api-keys/me), and immichfake's
# 2026-11-06, owner approved 2026-11-06: Kontaktfotos: contacts' pictures (14,434 -> 14,575):
# immich.Hochladen (archived), Funde (GET /api/faces), Entfernen (DELETE /api/assets), multipart and 204; immichfake's
# 2026-11-07, owner approved 2026-11-07: Befunde in der Box-Übersicht (14,575 -> 14,579): laib.Befunde,
# the last diag run's time and findings, in place of laib.Diag
# 2026-11-07, PENDING the owner's yes: Dieselbe Person, Kontext (14,579 -> 14,642): immich.Umfeld (POST
# /api/search/metadata with personIds, exif and people) and immichfake's search over a library
budget connectors 14643 $connectors   # one client per service, with its fake: 11,327, 12,543 with abs and musicbrainz
# 2026-11-04, internal/jsonfile (47 lines, 50 of cap) from household's cap to shared's: it
# replaces nine copies of a save, 62 of their lines in household
# 2026-11-05, owner 2026-11-05 yes: Immich for the contacts (9,583 -> 9,874):
# internal/gesichter, a contact matched to its Immich person, the faces fetched in the background, made
# 96 px and kept in HOMES_DATA, the suggestions and who was hidden from them
# 2026-11-06, owner approved 2026-11-06: Kontakte → Immich, every contact a person in Immich (9,874 -> 10,025): internal/gesichter
# matches the Haushalts-Kontakte and the key owner's Handy-Kontakte to Immich's people hourly, makes the
# missing ones, follows the ones it made, keeps the mapping, learns the owner's login (abgleich.go, 139);
# the suggestions and their hidden list go (gesichter.go 291 -> 303)
# 2026-11-06, owner approved 2026-11-06: Dieselbe Person: one name one person, „Dieselbe Person?“
# (10,025 -> 10,510): gesichter/dieselbe.go merges the persons of one name hourly and follows the mapping, asks Immich for the closest
# faces in the background, ranks the pairs, keeps „Nein“; gesichter.go keeps the key's permissions for the missing-permission line
# 2026-11-06, owner approved 2026-11-06: Kontaktfotos: „Ist das …?“ (10,510 -> 10,872):
# gesichter/kontaktbilder.go uploads the owner's contacts' pictures, follows what Immich made of each, asks, merges only on Ja
# 2026-11-07, PENDING the owner's yes: Dieselbe Person, leere Bilder, Kontext und Aufgabe (10,872 -> 11,088):
# a pair or question only with both faces kept as JPEGs and in a photo; gesichter/umfeld.go reads each shown
# person's places, years and companions at start and daily; Offen and einrichten.Fragen make the open
# questions one task; Öffnen and the icon per module in /napi/einrichtung
budget household 11089 $household      # the household's own logic: 10,638, 9,176 without them and the fetcher
# 2026-11-04, homes ADR 0001 step H1 (owner: „ganz durchziehen“): internal/modul, what an area
# says about itself and the list Homes builds from them, 1,472 with it; plus 5 %
# 2026-11-04, step H8 (§10): the traces and wishes in internal/modul, and internal/texte, the one
# table of the new family words (owner: Homes becomes multilingual), 1,709; plus 5 %
# 2026-11-04, the owner: „Texte aus Übersetzungen sind kein Code. Also raus aus der Grenze.“ The
# text catalog (internal/texte/katalog.go: the family's words by id, and later each language's
# table, de/en/es/fr) is words, not code, and multiplies with every language: not counted. The
# package stays shared for the import rules; its logic (texte.go: T, In, Titel) is counted here
budget shared 1795 $(echo "$shared" | sed 's#internal/texte ##') internal/texte/texte.go # the card, Markdown, the app's names: 1,019; abruf, namen: 1,287; jsonfile: 1,334

# every package in one area
for d in internal/*/; do
  d=${d%/}
  n=0
  for a in $connectors $shared $household internal/pages internal/napi; do [ "$a" = "$d" ] && n=$((n + 1)); done
  [ "$n" = 1 ] || { echo "$d is in $n areas, it belongs in exactly one (scripts/loc.sh)"; bad=1; }
done

# who imports whom
mod=github.com/mdopp/homes/
imports=$(go list -f '{{.ImportPath}} {{join .Imports " "}}' ./... | sed "s#$mod##g")
uses() { # uses <importer-regex> <imported-regex>: the lines that break the rule
  echo "$imports" | awk -v from="^($1)\$" -v to="^($2)\$" '$1 ~ from { for (i = 2; i <= NF; i++) if ($i ~ to) print $1 " imports " $i }'
}
re() { echo "$@" | sed 's/ *$//; s/ \{1,\}/|/g'; }
leaf=$(uses "($(re $shared))(/.*)?" '(internal|cmd)/.*')
[ -z "$leaf" ] || { echo "a shared package imports Homes:"; echo "$leaf"; bad=1; }
conn=$(uses "($(re $connectors))(/.*)?" '(internal|cmd)/.*' | grep -vE " imports ($(re $shared))\$" || true)
[ -z "$conn" ] || { echo "a connector imports more of Homes than shared packages:"; echo "$conn"; bad=1; }
surface=$(uses '.*' 'internal/(pages|napi)' | grep -vE '^cmd/|^internal/pages imports internal/napi$' || true)
[ -z "$surface" ] || { echo "only cmd imports pages, only cmd and pages import napi:"; echo "$surface"; bad=1; }
fakes=$(uses '.*' 'internal/.*fake' | grep -v '^cmd/homesdemo ' || true)
[ -z "$fakes" ] || { echo "a fake is for tests and cmd/homesdemo:"; echo "$fakes"; bad=1; }
exit $bad
