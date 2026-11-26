//go:build mod_dashboard

package main

// Pflichtmodul: tools/matrix setzt mod_dashboard in jeder Kombination. Ohne
// das Tag wird das Dashboard-Paket weder importiert noch kompiliert; der Kern
// zeigt dann unter /admin nur seine schlichte Willkommensseite.
import _ "github.com/julianirmler/styrixa-cms/internal/modules/dashboard"
