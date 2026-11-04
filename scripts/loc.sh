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
connectors=$(p abs bote caldav discogs ha jellyfin judge laib musicbrainz paperless qmd reitti samba werkstatt)
shared=$(p abruf app assistent cards code128 jsonfile markdown mcpserver namen)
household=$(p ansicht devices dokumente einkauf energie favoriten gesehen haushalt library merken notes papierkorb podcasts schalter takeout usage)

budget() { # budget <name> <cap> <dir…>
  name=$1 cap=$2; shift 2
  prod=$(find "$@" -name '*.go' ! -name '*_test.go' -exec cat {} + 2>/dev/null | wc -l)
  test=$(find "$@" -name '*_test.go' -exec cat {} + 2>/dev/null | wc -l)
  echo "$name production: $prod  tests: $test  (cap $cap)"
  [ "$prod" -lt "$cap" ] || { echo "$name over $cap lines"; bad=1; }
  [ "$((prod * 100))" -lt "$((cap * 98))" ] || [ "$prod" -ge "$cap" ] || echo "warning: $name at $prod of $cap lines, 98 % or more"
}
# area       cap    dirs              reason (2026-11-03: the size that day, plus 5 %)
budget pages 15419 internal/pages     # the family's pages, one per file: 14,684
budget napi 5033 internal/napi        # the app's widgets' and share sheet's API: 4,793
budget cmd 1069 cmd                   # homes' wiring and the demo on fakes: 1,018
# 2026-11-04, the caps moved with the packages, none raised (sum 24,134 as before): abs and
# musicbrainz (1,227 lines, 1,289 of cap) from household to connectors once merken's fetcher
# was internal/abruf (235 lines of merken, 247 of cap, to shared) and jellyfin.Plain
# internal/namen (11 lines, 12 of cap, to shared)
budget connectors 13171 $connectors   # one client per service, with its fake: 11,327, 12,543 with abs and musicbrainz
# 2026-11-04, internal/jsonfile (47 lines, 50 of cap) from household's cap to shared's: it
# replaces nine copies of a save, 62 of their lines in household
budget household 9584 $household      # the household's own logic: 10,638, 9,176 without them and the fetcher
budget shared 1379 $shared            # the card, Markdown, the app's names: 1,019; abruf, namen: 1,287; jsonfile: 1,334

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
