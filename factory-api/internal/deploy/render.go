package deploy

import (
	"fmt"
	"strings"
)

// SystemdUserUnit erzeugt einen systemd-User-Service (kein root nötig).
// base ist der absolute App-Ordner auf dem Server.
func SystemdUserUnit(service, base string, appPort int) string {
	return fmt.Sprintf(`# Erzeugt von der STYRIXA Factory – Änderungen werden beim nächsten Deploy überschrieben.
[Unit]
Description=STYRIXA CMS (%[1]s)
After=network-online.target

[Service]
Type=simple
WorkingDirectory=%[2]s/current
ExecStart=%[2]s/current/styrixa --addr 127.0.0.1:%[3]d
Environment=STYRIXA_DATA=%[2]s/shared/data
EnvironmentFile=-%[2]s/shared/styrixa.env
Restart=on-failure
RestartSec=2
NoNewPrivileges=yes
PrivateTmp=yes
MemoryMax=128M

[Install]
WantedBy=default.target
`, service, base, appPort)
}

// CaddySnippet / NginxSnippet: Reverse-Proxy-Vorschläge für die Domain.
func CaddySnippet(domain string, appPort int) string {
	return fmt.Sprintf("%s {\n\tencode zstd gzip\n\treverse_proxy 127.0.0.1:%d\n}\n", domain, appPort)
}

func NginxSnippet(domain string, appPort int) string {
	return fmt.Sprintf(`server {
    server_name %[1]s;
    client_max_body_size 64m;
    location / {
        proxy_pass http://127.0.0.1:%[2]d;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
`, domain, appPort)
}

// RootHtaccess leitet im CGI-Modus alle Anfragen, die keine echte Datei sind,
// an styrixa.cgi weiter. Statische Dateien (Theme-Assets, Uploads) liefert Apache direkt.
func RootHtaccess(dataDir string) string {
	return fmt.Sprintf(`# BEGIN STYRIXA – erzeugt von der STYRIXA Factory
Options +ExecCGI -Indexes
AddHandler cgi-script .cgi
DirectoryIndex styrixa.cgi
SetEnv STYRIXA_DATA %[1]s
SetEnv STYRIXA_MODE cgi

<IfModule mod_rewrite.c>
RewriteEngine On
RewriteRule ^%[2]s(/|$) - [F,L]
RewriteRule ^styrixa\.cgi$ - [L]
RewriteCond %%{REQUEST_FILENAME} !-f
RewriteRule ^(.*)$ styrixa.cgi/$1 [L,QSA]
</IfModule>

<FilesMatch "\.(db|db-wal|db-shm|env|log)$">
  <IfModule mod_authz_core.c>
    Require all denied
  </IfModule>
  <IfModule !mod_authz_core.c>
    Order allow,deny
    Deny from all
  </IfModule>
</FilesMatch>
# END STYRIXA
`, dataDir, regexpQuote(dataDir))
}

// DenyAllHtaccess schützt den Datenordner (styrixa.db) vor direktem Abruf.
const DenyAllHtaccess = `# STYRIXA – kein Webzugriff auf Daten
<IfModule mod_authz_core.c>
  Require all denied
</IfModule>
<IfModule !mod_authz_core.c>
  Order allow,deny
  Deny from all
</IfModule>
`

func regexpQuote(s string) string {
	r := strings.NewReplacer(".", `\.`, "+", `\+`, "-", `\-`)
	return r.Replace(s)
}
