package core

import (
	"net"
	"net/http"
	"os"
	"strings"
)

// ClientIP liefert die Adresse des Besuchers für das Rate-Limit.
//
// Im Server-Modus läuft das CMS hinter Caddy/nginx auf 127.0.0.1 (SSH-Deploy).
// Nur wenn die Verbindung von Loopback kommt, gilt deshalb der letzte Eintrag in
// X-Forwarded-For – den hat der lokale Proxy gesetzt bzw. angehängt. Von jeder
// anderen Adresse wird der Header ignoriert, sonst könnte ihn jeder fälschen.
// Im CGI-Modus ist RemoteAddr das REMOTE_ADDR von Apache.
func ClientIP(r *http.Request) string {
	peer := hostOnly(r.RemoteAddr)
	if !isLoopback(peer) {
		return peer
	}
	xff := r.Header.Values("X-Forwarded-For")
	if len(xff) == 0 {
		return peer
	}
	parts := strings.Split(xff[len(xff)-1], ",")
	last := strings.TrimSpace(parts[len(parts)-1])
	if ip := net.ParseIP(hostOnly(last)); ip != nil {
		return ip.String()
	}
	return peer
}

// requestScheme ermittelt http/https: direkte TLS-Verbindung, HTTPS=on im
// CGI-Modus (Apache) oder X-Forwarded-Proto vom lokalen Proxy.
func requestScheme(r *http.Request) string {
	switch {
	case r.TLS != nil:
		return "https"
	case os.Getenv("GATEWAY_INTERFACE") != "" && strings.EqualFold(os.Getenv("HTTPS"), "on"):
		return "https"
	case isLoopback(hostOnly(r.RemoteAddr)) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"):
		return "https"
	}
	return "http"
}

func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return strings.Trim(addr, "[]")
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// isLocalHost: Host-Header ohne Port ist localhost/127.0.0.1/::1 – dann darf
// das Session-Cookie ohne Secure gesetzt werden (lokale Tests ohne TLS).
func isLocalHost(host string) bool {
	h := hostOnly(host)
	return h == "localhost" || isLoopback(h)
}
