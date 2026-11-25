import { test } from "node:test";
import assert from "node:assert/strict";
import { normalize, queryFor, toggle } from "./selection.js";

const MODULES = [
  { id: "dashboard", name: "Dashboard", required: true },
  { id: "content", name: "Content Management", required: true },
  { id: "media", name: "Media Management" },
  { id: "seo", name: "SEO & GEO Management" },
  { id: "design", name: "Design Management" },
  { id: "themes", name: "Theme Management", requires: ["design"] },
  { id: "shop", name: "Shop Management", requires: ["media"] },
];

const sorted = (s) => [...s].sort();

test("Pflichtmodule immer aktiv, unbekannte IDs entfernt", () => {
  assert.deepEqual(sorted(normalize(new Set(["xyz"]), MODULES)), ["content", "dashboard"]);
});

test("Pflichtmodul lässt sich nicht abwählen", () => {
  const { selected, notice } = toggle(new Set(["dashboard", "content"]), "dashboard", MODULES);
  assert.ok(selected.has("dashboard"));
  assert.equal(notice, null);
});

test("Abhängigkeiten werden aktiviert", () => {
  for (const [id, dep, name] of [
    ["shop", "media", "Media Management"],
    ["themes", "design", "Design Management"],
  ]) {
    const { selected, notice } = toggle(new Set(), id, MODULES);
    assert.ok(selected.has(id) && selected.has(dep), id);
    assert.match(notice, new RegExp(`^${name} automatisch aktiviert`));
  }
});

test("ohne fehlende Abhängigkeit kein Hinweis", () => {
  const { selected, notice } = toggle(new Set(["media"]), "shop", MODULES);
  assert.ok(selected.has("shop"));
  assert.equal(notice, null);
});

test("Abwählen einer Abhängigkeit entfernt Abhängige", () => {
  const { selected, notice } = toggle(new Set(["media", "shop", "seo"]), "media", MODULES);
  assert.deepEqual(sorted(selected), ["content", "dashboard", "seo"]);
  assert.match(notice, /^Shop Management wurde mit abgewählt/);
});

test("Abwählen ohne Abhängige", () => {
  const { selected, notice } = toggle(new Set(["media", "shop"]), "shop", MODULES);
  assert.ok(selected.has("media") && !selected.has("shop"));
  assert.equal(notice, null);
});

test("unbekannte ID wird ignoriert", () => {
  const { selected, notice } = toggle(new Set(["seo"]), "gibtsnicht", MODULES);
  assert.deepEqual(sorted(selected), ["content", "dashboard", "seo"]);
  assert.equal(notice, null);
});

test("Eingabe-Set wird nicht verändert", () => {
  const input = new Set(["media"]);
  toggle(input, "media", MODULES);
  assert.deepEqual(sorted(input), ["media"]);
});

test("Query-Reihenfolge stabil", () => {
  const a = queryFor(new Set(["shop", "media", "dashboard"]), "linux/amd64");
  const b = queryFor(new Set(["dashboard", "media", "shop"]), "linux/amd64");
  assert.equal(a, b);
  assert.equal(a, "modules=dashboard%2Cmedia%2Cshop&target=linux%2Famd64");
  const p = new URLSearchParams(a);
  assert.equal(p.get("modules"), "dashboard,media,shop");
  assert.equal(p.get("target"), "linux/amd64");
});
