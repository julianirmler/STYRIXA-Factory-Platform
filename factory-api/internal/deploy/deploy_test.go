package deploy

import (
	"context"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	ok := Target{Method: MethodSSH, Host: "server.kunde.at", User: "deploy", RemoteDir: "~/styrixa"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if ok.Port != 22 || ok.AppPort != 8080 {
		t.Fatalf("defaults: %+v", ok)
	}
	bad := []Target{
		{Method: MethodSSH, Host: "ssh://x", User: "u"},
		{Method: MethodSSH, Host: "x", User: "u; rm -rf /"},
		{Method: MethodSSH, Host: "x", User: "u", RemoteDir: "../etc"},
		{Method: MethodSSH, Host: "x", User: "u", RemoteDir: "a b"},
		{Method: MethodSSH, Host: "-oProxyCommand=x", User: "u"},
		{Method: MethodFTP, Host: "x", User: "u", PublicURL: "https://k.at", TLS: FTPPlain},
		{Method: MethodFTP, Host: "x", User: "u"},
		{Method: MethodFTP, Host: "x", User: "u", PublicURL: "https://k.at", DataDir: "/abs"},
	}
	for i, b := range bad {
		if err := b.Validate(); err == nil {
			t.Errorf("case %d: Fehler erwartet für %+v", i, b)
		}
	}
}

func TestHelpers(t *testing.T) {
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Error(got)
	}
	if got := ServiceName("Kunde CMS!"); got != "styrixa-kunde-cms" {
		t.Error(got)
	}
	for in, want := range map[string]string{"Linux x86_64\n": "linux/amd64", "Linux aarch64": "linux/arm64", "Darwin arm64": "darwin/arm64"} {
		os, arch := platformFromUname(in)
		if os+"/"+arch != want {
			t.Errorf("%q → %s/%s", in, os, arch)
		}
	}
	h := RootHtaccess("styrixa-data")
	for _, must := range []string{"AddHandler cgi-script .cgi", `^styrixa\-data(/|$) - [F,L]`, "SetEnv STYRIXA_DATA styrixa-data", "RewriteRule ^(.*)$ styrixa.cgi/$1"} {
		if !strings.Contains(h, must) {
			t.Errorf(".htaccess fehlt %q", must)
		}
	}
	if u := SystemdUserUnit("styrixa-x", "/home/u/styrixa", 8081); !strings.Contains(u, "ExecStart=/home/u/styrixa/current/styrixa --addr 127.0.0.1:8081") {
		t.Error(u)
	}
}

func TestPublicOnly(t *testing.T) {
	for _, a := range []string{"127.0.0.1:22", "10.0.0.5:21", "192.168.1.1:22", "169.254.169.254:80", "100.64.1.1:22", "[::1]:22", "0.0.0.0:22"} {
		if publicOnly("tcp", a, nil) == nil {
			t.Errorf("%s hätte gesperrt werden müssen", a)
		}
	}
	if err := publicOnly("tcp", "93.184.216.34:22", nil); err != nil {
		t.Error(err)
	}
}

func TestSSRFBlockedByDefault(t *testing.T) {
	_, err := DeployFTP(context.Background(), Artifact{GOOS: "linux", GOARCH: "amd64", BinaryPath: "/dev/null"},
		Target{Method: MethodFTP, Host: "127.0.0.1", Port: 21, User: "u", PublicURL: "https://x.at"},
		Credentials{Password: "p"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "keine öffentliche Adresse") {
		t.Fatalf("SSRF-Sperre erwartet, bekam %v", err)
	}
}
