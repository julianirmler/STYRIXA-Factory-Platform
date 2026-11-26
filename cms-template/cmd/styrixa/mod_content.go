//go:build mod_content

package main

// Pflichtmodul: tools/matrix setzt mod_content in jeder Kombination. Ohne das
// Tag wird das Content-Paket weder importiert noch kompiliert.
import _ "github.com/julianirmler/styrixa-cms/internal/modules/content"
