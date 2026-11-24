//go:build mod_shop && !mod_media

package main

// Bewusster Compile-Fehler: Der Shop braucht Media (Produktbilder).
// Fehlermeldung: "undefined: ERROR_mod_shop_requires_mod_media"
var _ = ERROR_mod_shop_requires_mod_media
