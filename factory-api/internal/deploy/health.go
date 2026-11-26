package deploy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// waitHealthy ruft url wiederholt auf, bis /healthz mit 200 antwortet.
func waitHealthy(ctx context.Context, client *http.Client, url string, attempts int, pause time.Duration) error {
	var last error
	for i := 0; i < attempts; i++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		req.Header.Set("User-Agent", "STYRIXA-Factory-Deploy/1")
		resp, err := client.Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			if t := htmlTitle(body); t != "" {
				last = fmt.Errorf("HTTP %d%s", resp.StatusCode, t)
			} else {
				last = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			}
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pause):
		}
	}
	return fmt.Errorf("health-check %s fehlgeschlagen: %w", url, last)
}

// htmlTitle liefert „ (Titel)“ einer HTML-Fehlerseite statt des ganzen Markups.
func htmlTitle(b []byte) string {
	s := string(b)
	i := strings.Index(strings.ToLower(s), "<title>")
	if i < 0 {
		return ""
	}
	s = s[i+len("<title>"):]
	if j := strings.Index(strings.ToLower(s), "</title>"); j >= 0 {
		s = s[:j]
	}
	return " (" + strings.TrimSpace(s) + ")"
}
