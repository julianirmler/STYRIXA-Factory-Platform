package deploy

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// publicOnly verhindert SSRF: Die Factory verbindet sich nur mit öffentlichen
// Adressen – nicht mit localhost, privaten Netzen, Link-Local (Cloud-Metadaten)
// oder CGNAT. Geprüft wird die tatsächlich gewählte IP (schützt vor DNS-Rebinding).
func publicOnly(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || cgnat.Contains(ip) {
		return fmt.Errorf("Ziel %s ist keine öffentliche Adresse – Deployment nicht erlaubt", host)
	}
	return nil
}

func guardedDialer(allowPrivate bool) net.Dialer {
	d := net.Dialer{Timeout: 15 * time.Second}
	if !allowPrivate {
		d.Control = publicOnly
	}
	return d
}

func guardedHTTPClient(allowPrivate bool) *http.Client {
	d := guardedDialer(allowPrivate)
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{DialContext: d.DialContext, TLSHandshakeTimeout: 10 * time.Second},
	}
}
