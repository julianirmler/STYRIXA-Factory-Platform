# Hoster-Probe

Prüft in einer Minute, ob ein PHP-Webspace (z. B. easyname) das STYRIXA CMS über die PHP-Brücke betreiben kann.

1. `styrixa-probe.php` per FTP in den Document-Root einer **Test-Subdomain** laden, am besten unter einem zufälligen Namen (z. B. `probe-7f3k.php`).
2. Im Browser `https://<test-subdomain>/probe-7f3k.php` öffnen.
3. Ausgabe kopieren, **Datei sofort löschen** (sie zeigt Serverdetails).

Entscheidend sind `proc_open: ja`, `exec_own_binary: ja` und `sqlite_wal` nicht `NEIN`.
