<?php
// STYRIXA Hoster-Probe – prüft, ob ein PHP-Webspace die PHP-Brücke tragen kann.
// Einmalig hochladen, im Browser aufrufen, Ergebnis kopieren, Datei SOFORT wieder löschen.
// Führt nur feste Befehle aus (/bin/true, /bin/echo, ein kopiertes /bin/true), keine Eingaben aus der Anfrage.
header('Content-Type: text/plain; charset=utf-8');
header('Cache-Control: no-store');
header('X-Robots-Tag: noindex');

$r = [];
$disabled = array_map('trim', explode(',', (string) ini_get('disable_functions')));
$fn = fn($f) => function_exists($f) && !in_array($f, $disabled, true);

$r['php_version']       = PHP_VERSION;
$r['sapi']              = PHP_SAPI;
$r['os']                = PHP_OS_FAMILY . ' ' . php_uname('m');
$r['proc_open']         = $fn('proc_open') ? 'ja' : 'NEIN';
$r['disable_functions'] = ini_get('disable_functions') ?: '(keine)';
$r['open_basedir']      = ini_get('open_basedir') ?: '(keine Einschränkung)';
$r['max_execution_time']= ini_get('max_execution_time');
$r['memory_limit']      = ini_get('memory_limit');
$r['post_max_size']     = ini_get('post_max_size');
$r['upload_max_filesize']= ini_get('upload_max_filesize');
$r['script_dir']        = __DIR__;
$r['user']              = function_exists('posix_geteuid') ? (posix_getpwuid(posix_geteuid())['name'] ?? posix_geteuid()) : get_current_user();

// 1. Systembefehl starten
if ($fn('proc_open')) {
    $p = @proc_open(['/bin/echo', 'styrixa-ok'], [1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes);
    if (is_resource($p)) {
        $out = trim(stream_get_contents($pipes[1])); fclose($pipes[1]); fclose($pipes[2]);
        $r['exec_system_binary'] = (proc_close($p) === 0 && $out === 'styrixa-ok') ? 'ja' : 'NEIN';
    } else {
        $r['exec_system_binary'] = 'NEIN (proc_open fehlgeschlagen)';
    }

    // 2. Eigenes Binary im Webspace ausführbar? (noexec-Mount erkennen)
    $copy = __DIR__ . '/.styrixa-probe-bin';
    if (@copy('/bin/true', $copy) && @chmod($copy, 0700)) {
        $p = @proc_open([$copy], [1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes);
        if (is_resource($p)) {
            fclose($pipes[1]); fclose($pipes[2]);
            $r['exec_own_binary'] = proc_close($p) === 0 ? 'ja' : 'NEIN (Exit-Code ≠ 0)';
        } else {
            $r['exec_own_binary'] = 'NEIN (Start verweigert – Webspace vermutlich noexec)';
        }
        @unlink($copy);
    } else {
        $r['exec_own_binary'] = 'unbekannt (/bin/true nicht kopierbar – open_basedir?)';
    }
} else {
    $r['exec_system_binary'] = 'NEIN (proc_open gesperrt)';
    $r['exec_own_binary']    = 'NEIN (proc_open gesperrt)';
}

// 3. SQLite im WAL-Modus auf diesem Dateisystem?
if (class_exists('PDO') && in_array('sqlite', PDO::getAvailableDrivers(), true)) {
    $db = __DIR__ . '/.styrixa-probe.db';
    try {
        $pdo = new PDO('sqlite:' . $db);
        $mode = $pdo->query('PRAGMA journal_mode=WAL')->fetchColumn();
        $pdo->exec('CREATE TABLE t(x); INSERT INTO t VALUES(1);');
        $r['sqlite_wal'] = strtolower((string) $mode) === 'wal' ? 'ja' : "NEIN (Modus: $mode)";
        $pdo = null;
    } catch (Throwable $e) {
        $r['sqlite_wal'] = 'NEIN (' . $e->getMessage() . ')';
    }
    foreach (['', '-wal', '-shm'] as $s) { @unlink($db . $s); }
} else {
    $r['sqlite_wal'] = 'unbekannt (PHP ohne pdo_sqlite – für STYRIXA nicht nötig)';
}

// 4. mod_rewrite (nur bei Apache-Modul sichtbar)
$r['mod_rewrite'] = function_exists('apache_get_modules')
    ? (in_array('mod_rewrite', apache_get_modules(), true) ? 'ja' : 'NEIN')
    : 'unbekannt (PHP-FPM – wird beim Deploy getestet)';

$ok = $r['exec_own_binary'] === 'ja' && $r['sqlite_wal'] !== 'NEIN';
echo "STYRIXA Hoster-Probe\n====================\n";
foreach ($r as $k => $v) { printf("%-20s %s\n", $k . ':', $v); }
echo "\nErgebnis: " . ($ok ? "PHP-Brücke möglich ✓" : "PHP-Brücke NICHT möglich ✗") . "\n";
echo "\nBitte diese Datei jetzt wieder vom Webspace löschen.\n";
