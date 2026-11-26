package main

import "strings"

// testTags sind die Modul-Tags, mit denen dieser Test kompiliert wurde
// (je Tag eine Datei buildtags_<id>_test.go). Tests, die ein Binary bauen,
// geben sie weiter – sonst vergleichen sie zwei verschiedene CMS.
var testTags []string

func buildTags() string { return strings.Join(testTags, " ") }
