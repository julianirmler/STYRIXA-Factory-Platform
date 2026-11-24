//go:build mod_shop

package main

// Ohne -tags mod_shop wird das Shop-Paket weder importiert noch kompiliert.
import _ "github.com/julianirmler/styrixa-cms/internal/modules/shop"
