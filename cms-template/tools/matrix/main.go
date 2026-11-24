// matrix liest modules.json und gibt alle gültigen Modul-Kombinationen aus.
//
//	go run ./tools/matrix              → eine Zeile pro Kombination: "<key> <tags>"
//	go run ./tools/matrix -invalid     → "<key> <tags> <module-ohne-abhängigkeit>" (Build MUSS scheitern)
//	go run ./tools/matrix -targets     → Zielplattformen
//
// key ist der stabile Artefaktname, z. B. "media-seo-shop" (Pflichtmodule weggelassen, "core" = nur Pflicht).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

type module struct {
	ID       string   `json:"id"`
	Required bool     `json:"required"`
	Requires []string `json:"requires"`
}

type manifest struct {
	Modules []module `json:"modules"`
	Targets []string `json:"targets"`
}

func main() {
	file := flag.String("f", "modules.json", "Manifest")
	invalid := flag.Bool("invalid", false, "ungültige Kombinationen ausgeben")
	targets := flag.Bool("targets", false, "Zielplattformen ausgeben")
	flag.Parse()

	b, err := os.ReadFile(*file)
	if err != nil {
		fail(err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		fail(err)
	}
	if *targets {
		fmt.Println(strings.Join(m.Targets, "\n"))
		return
	}
	var req, opt []module
	for _, x := range m.Modules {
		if x.Required {
			req = append(req, x)
		} else {
			opt = append(opt, x)
		}
	}
	for _, line := range combos(req, opt, *invalid) {
		fmt.Println(line)
	}
}

func combos(req, opt []module, invalid bool) []string {
	var out []string
	for mask := 0; mask < 1<<len(opt); mask++ {
		on := map[string]bool{}
		for _, r := range req {
			on[r.ID] = true
		}
		var picked []string
		for i, o := range opt {
			if mask&(1<<i) != 0 {
				on[o.ID] = true
				picked = append(picked, o.ID)
			}
		}
		var violators []string // Module, deren Abhängigkeit fehlt
		for _, id := range picked {
			for _, o := range opt {
				if o.ID == id {
					for _, d := range o.Requires {
						if !on[d] {
							violators = append(violators, id)
						}
					}
				}
			}
		}
		ok := len(violators) == 0
		if ok == invalid {
			continue
		}
		var tags []string
		for id := range on {
			tags = append(tags, "mod_"+id)
		}
		sort.Strings(tags)
		sort.Strings(picked)
		key := strings.Join(picked, "-")
		if key == "" {
			key = "core"
		}
		line := key + " " + strings.Join(tags, ",")
		if invalid {
			line += " " + strings.Join(violators, ",")
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "matrix:", err)
	os.Exit(1)
}
