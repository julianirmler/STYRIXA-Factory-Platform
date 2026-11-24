// Testprogramm, das ein STYRIXA-Binary imitiert: /healthz auf --addr.
package main

import (
	"flag"
	"net/http"
	"os"
)

var fail = "" // per -ldflags "-X main.fail=1": beendet sich sofort (kaputtes Release)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "")
	flag.Parse()
	if fail != "" {
		os.Exit(3)
	}
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	_ = http.ListenAndServe(*addr, nil)
}
