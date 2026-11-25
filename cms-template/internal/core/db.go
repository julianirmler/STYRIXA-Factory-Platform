package core

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modernc.org/sqlite" // reines Go, CGO-frei (ADR-009); registriert Treiber "sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// DBFile ist der Dateiname der Datenbank im Datenordner (STYRIXA_DATA).
const DBFile = "styrixa.db"

// OpenDB öffnet (bzw. erzeugt) <dataDir>/styrixa.db.
//
// Alle Pragmas stehen im DSN, damit sie für jede Verbindung des Pools gelten:
//   - busy_timeout: bis zu 5 s auf Sperren anderer Prozesse warten
//   - WAL (siehe ensureWAL): Leser blockieren Schreiber nicht
//   - _txlock=immediate: Schreib-Transaktionen nehmen die Sperre bei BEGIN. Sonst
//     scheitert das Hochstufen von Lese- auf Schreibsperre bei parallelen
//     Prozessen sofort mit SQLITE_BUSY – busy_timeout greift dort nicht.
func OpenDB(dataDir string) (*sql.DB, error) {
	path, err := filepath.Abs(filepath.Join(dataDir, DBFile))
	if err != nil {
		return nil, err
	}
	if strings.ContainsRune(path, '?') {
		return nil, errors.New("db: Pfad des Datenordners darf kein „?“ enthalten")
	}
	// Datei selbst anlegen, damit sie nur für den Eigentümer lesbar ist.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	f.Close()

	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}

	if err := ensureWAL(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// ensureWAL schaltet die Datenbank einmalig auf WAL. Der Modus wird in der
// Datei gespeichert, danach ist nur noch eine Lese-Abfrage nötig.
//
// Das Umschalten braucht eine exklusive Sperre und wartet dabei nicht auf
// busy_timeout: Starten zwei Prozesse gleichzeitig auf einer neuen Datenbank,
// bekommt einer sofort SQLITE_BUSY. Deshalb hier kurz wiederholen.
//
// SQLite meldet keinen Fehler, wenn WAL nicht geht (z. B. auf
// Netzwerk-Dateisystemen), sondern bleibt beim alten Modus. Nicht stillschweigend
// weitermachen: ohne WAL sind parallele CGI-Prozesse nicht sicher.
func ensureWAL(db *sql.DB) error {
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		return fmt.Errorf("db: %w", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.EqualFold(mode, "wal") {
		err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode)
		if err == nil {
			if !strings.EqualFold(mode, "wal") {
				return fmt.Errorf("db: WAL-Modus nicht verfügbar (Modus %q) – der Datenordner muss auf einem lokalen Dateisystem liegen", mode)
			}
			break
		}
		var se *sqlite.Error
		if !errors.As(err, &se) || se.Code()&0xff != sqlite3.SQLITE_BUSY || time.Now().After(deadline) {
			return fmt.Errorf("db: WAL-Modus: %w", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}
