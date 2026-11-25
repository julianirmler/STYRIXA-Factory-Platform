import { test } from "node:test";
import assert from "node:assert/strict";
import { formatSize, moduleMeta, targetMeta } from "./moduleMeta.js";

test("bekannte und unbekannte Modul-IDs", () => {
  assert.ok(moduleMeta("shop").features.length > 0);
  const f = moduleMeta("neu");
  assert.ok(f.icon && f.desc);
  assert.equal(moduleMeta("toString"), f);
});

test("Plattform-Fallback", () => {
  assert.equal(targetMeta("linux/amd64").label, "Linux");
  assert.deepEqual(targetMeta("freebsd/riscv64"), { label: "freebsd", sub: "riscv64" });
});

test("Größen", () => {
  assert.equal(formatSize(512), "512 B");
  assert.equal(formatSize(12.5 * 1024 * 1024), "12,5 MB");
});
