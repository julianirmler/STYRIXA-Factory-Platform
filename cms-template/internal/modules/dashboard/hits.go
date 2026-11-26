package dashboard

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

// Zählung (Session 3.4): Tag × Pfad per UPSERT. Gespeichert werden nur
// Summen – keine IP, kein User-Agent, keine Cookies.

const (
	dayFormat   = "2006-01-02"
	maxPathLen  = 255
	keepDays    = 400 // ältere Tage löscht der Job dashboard.prune_hits
	topPagesMax = 10
)

// botMarkers: Teile von User-Agents automatisierter Abrufe (klein
// geschrieben). Bewusst einfach – es geht um ehrliche Zahlen, nicht um
// Abwehr. Leere User-Agents zählen ebenfalls nicht.
var botMarkers = []string{
	"bot", "crawl", "spider", "slurp", "scrape", "fetch", "preview",
	"facebookexternalhit", "embedly", "whatsapp", "telegram", "discord", "skype",
	"curl", "wget", "httpie", "python", "go-http-client", "java/", "okhttp", "axios", "node-fetch", "libwww", "perl",
	"headless", "phantomjs", "lighthouse", "pagespeed", "gtmetrix", "pingdom", "uptime", "monitor", "statuscake",
}

func isBot(ua string) bool {
	ua = strings.ToLower(strings.TrimSpace(ua))
	if ua == "" {
		return true
	}
	for _, m := range botMarkers {
		if strings.Contains(ua, m) {
			return true
		}
	}
	return false
}

type store struct {
	db  *sql.DB
	now func() time.Time // nil = time.Now
}

func (s *store) today() time.Time {
	t := time.Now()
	if s.now != nil {
		t = s.now()
	}
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// count zählt einen Aufruf von h.Path, wenn er nicht von einem Bot stammt
// und nicht unter /admin liegt (der Kern meldet dort ohnehin nichts).
func (s *store) count(ctx context.Context, h core.Hit) error {
	path := h.Path
	if isBot(h.UserAgent) || !strings.HasPrefix(path, "/") || path == "/admin" || strings.HasPrefix(path, "/admin/") ||
		len(path) > maxPathLen || !utf8.ValidString(path) {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO dashboard_hits (day, path, count) VALUES (?, ?, 1)
		ON CONFLICT(day, path) DO UPDATE SET count = count + 1`, s.today().Format(dayFormat), path)
	return err
}

// since liefert den ersten Tag eines Zeitraums von n Tagen bis heute.
func (s *store) since(n int) string {
	return s.today().AddDate(0, 0, -(n - 1)).Format(dayFormat)
}

func (s *store) total(ctx context.Context, days int) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT IFNULL(SUM(count), 0) FROM dashboard_hits WHERE day >= ?`, s.since(days)).Scan(&n)
	return n, err
}

type dayCount struct {
	Day   string // „Mo 23.11.“
	Count int64
}

// daily liefert die letzten n Tage, ältester zuerst, auch Tage ohne Aufrufe.
func (s *store) daily(ctx context.Context, days int) ([]dayCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT day, SUM(count) FROM dashboard_hits WHERE day >= ? GROUP BY day`, s.since(days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDay := map[string]int64{}
	for rows.Next() {
		var d string
		var n int64
		if err := rows.Scan(&d, &n); err != nil {
			return nil, err
		}
		byDay[d] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	weekday := [...]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}
	out := make([]dayCount, 0, days)
	for i := days - 1; i >= 0; i-- {
		t := s.today().AddDate(0, 0, -i)
		out = append(out, dayCount{Day: weekday[t.Weekday()] + " " + t.Format("02.01."), Count: byDay[t.Format(dayFormat)]})
	}
	return out, nil
}

type pageCount struct {
	Path  string
	Count int64
}

func (s *store) topPages(ctx context.Context, days int) ([]pageCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path, SUM(count) AS n FROM dashboard_hits WHERE day >= ?
		GROUP BY path ORDER BY n DESC, path LIMIT ?`, s.since(days), topPagesMax)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pageCount
	for rows.Next() {
		var p pageCount
		if err := rows.Scan(&p.Path, &p.Count); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// dbSize: Größe der Datenbank in Bytes (Seiten × Seitengröße, ohne WAL).
func (s *store) dbSize(ctx context.Context) (int64, error) {
	var pages, size int64
	if err := s.db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
		return 0, err
	}
	err := s.db.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&size)
	return pages * size, err
}

// prune löscht Tage, die älter als keepDays sind.
func (s *store) prune(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM dashboard_hits WHERE day < ?`, s.since(keepDays))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
