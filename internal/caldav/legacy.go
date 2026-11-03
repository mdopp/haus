package caldav

import "strings"

// legacyAccount is legacy: the household's Radicale/LLDAP account before
// the rename to homes (owner 2026-11-03, laib#1021). Its collections moved
// inside Radicale from /haus/… to /homes/…, contents and UIDs carried.
const legacyAccount = "haus"

// LegacyAccountPath is legacy: a collection path Homes stored while the
// account was haus (ansicht.json's colours and hidden calendars, a deleted
// calendar in the Papierkorb) read as the account it is now, user
// (HOMES_RADICALE_USER). Any other path, and any path when user is haus or
// unset, is returned as it is. It ends with laib#1021 step 9, once the
// stored values have been written under the new account.
func LegacyAccountPath(path, user string) string {
	if user == "" || user == legacyAccount {
		return path
	}
	if rest, ok := strings.CutPrefix(path, "/"+legacyAccount+"/"); ok {
		return "/" + user + "/" + rest
	}
	return path
}
