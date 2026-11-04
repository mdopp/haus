# Google-Daten übernehmen (Takeout)

So kommen Kalender, Kontakte und Notizen aus einem Google-Konto einmalig
auf die Box. Termine landen im gemeinsamen Kalender „Haushalt“, Kontakte
im gemeinsamen Adressbuch „Haushalt“ (beide sehen alle, die sie auf dem
Telefon eingerichtet haben), Google Notizen als Notizen im Ordner
`notes/Google Notizen` der Dateiablage, Bilder daraus in
`notes/Google Notizen/Anhänge`.

1. **Exportieren:** Auf takeout.google.com nur *Kalender*, *Kontakte* und
   *Notizen* (Google Notizen/Keep) auswählen, Format ZIP, exportieren und
   die ZIP-Datei(en) herunterladen. Große Exporte kommen als mehrere
   Dateien (`…-001.zip`, `…-002.zip`); alle gehören zusammen.
2. **Auf die Box legen:** die Datei(en) in den Datenordner von Homes
   kopieren, das ist `<dataDir>/homes` (`dataDir` steht in der `box.yml`,
   z. B. `/mnt/data/stacks/homes`). Im Homes-Container heißt der Ordner
   `/data`.
3. **Probelauf:** zeigt nur, was übernommen würde, und schreibt nichts:

   ```
   laib exec homes --container systemd-homes -- /homes import-takeout -dry-run /data/takeout-20261015T100000Z-001.zip /data/takeout-20261015T100000Z-002.zip
   ```

   Einen Kalender, den die Telefone sowieso zeigen (etwa die Feiertage),
   lässt `-skip-calendar "Feiertage in Deutschland"` weg; mehrfach möglich.
4. **Übernehmen:** derselbe Befehl ohne `-dry-run`. Am Ende steht, wie
   viele Termine, Kontakte und Notizen neu sind, wie viele schon da waren
   und was nicht ging, mit Grund.
5. **Aufräumen:** die ZIP-Dateien aus `<dataDir>/homes` löschen. Sie
   enthalten persönliche Daten und würden sonst mitgesichert.

Gut zu wissen:
- **Ein zweiter Lauf schadet nicht.** Was schon da ist, wird nicht noch
  einmal angelegt und nicht überschrieben, auch nicht, wenn es inzwischen
  auf einem Telefon geändert wurde. Nach einem abgebrochenen Lauf einfach
  noch einmal starten.
- **Kontakte, die in Google in mehreren Gruppen stehen** („Alle Kontakte“,
  „Meine Kontakte“, eigene Labels), kommen nur einmal an. Ein Kontakt ohne
  Namen bekommt seine Nummer oder E-Mail als Namen; einer ganz ohne Namen,
  Nummer und E-Mail wird ausgelassen und genannt.
- **Notizen aus dem Papierkorb** von Google Notizen werden nicht
  übernommen; archivierte schon, mit dem Hinweis „In Keep archiviert“.
- **Die Suche** findet die übernommenen Notizen nach dem nächsten
  Auffrischen der Bibliothek, spätestens nach einer Viertelstunde.
- **Ohne Radicale-Konto** für Homes (`RADICALE_USER` und `laib secret set
  HOMES_RADICALE_PASSWORD`) werden Kalender und Kontakte ausgelassen; der
  Befehl sagt das.
- `laib exec` macht vorher eine Sicherung und zählt als Eingriff ins
  Stunden-Budget von Homes.

