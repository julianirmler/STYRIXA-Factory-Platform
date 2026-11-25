import { test } from "node:test";
import assert from "node:assert/strict";
import { createSSEParser, readSSE } from "./sse.js";

function collect(chunks) {
  const out = [];
  const p = createSSEParser((e) => out.push(e));
  chunks.forEach((c) => p.push(c));
  p.end();
  return out;
}

const STREAM = 'event: log\ndata: {"msg":"a"}\n\nevent: result\ndata: {"ok":true}\n\n';
const EXPECTED = [
  { event: "log", data: '{"msg":"a"}' },
  { event: "result", data: '{"ok":true}' },
];

test("ganzer Stream", () => {
  assert.deepEqual(collect([STREAM]), EXPECTED);
});

test("jede Chunk-Grenze", () => {
  for (let i = 1; i < STREAM.length; i++) {
    assert.deepEqual(collect([STREAM.slice(0, i), STREAM.slice(i)]), EXPECTED, `Schnitt bei ${i}`);
  }
});

test("zeichenweise", () => {
  assert.deepEqual(collect([...STREAM]), EXPECTED);
});

test("\\r\\n und \\r als Zeilenende, auch über Chunk-Grenzen", () => {
  const crlf = STREAM.replaceAll("\n", "\r\n");
  assert.deepEqual(collect([...crlf]), EXPECTED);
  for (let i = 1; i < crlf.length; i++) {
    assert.deepEqual(collect([crlf.slice(0, i), crlf.slice(i)]), EXPECTED, `Schnitt bei ${i}`);
  }
  assert.deepEqual(collect([STREAM.replaceAll("\n", "\r")]), EXPECTED);
});

test("Kommentare, mehrzeilige Daten, Standard-Ereignis, fehlende Leerzeile am Ende", () => {
  const out = collect([": ping\n\ndata: x\ndata: y\n\nevent: error\ndata:{}"]);
  assert.deepEqual(out, [
    { event: "message", data: "x\ny" },
    { event: "error", data: "{}" },
  ]);
});

test("readSSE mit UTF-8 über Byte-Grenzen", async () => {
  const bytes = new TextEncoder().encode('event: log\ndata: {"msg":"Größe ✓"}\n\n');
  const body = new ReadableStream({
    start(c) {
      for (const b of bytes) c.enqueue(new Uint8Array([b]));
      c.close();
    },
  });
  const out = [];
  await readSSE(body, (e) => out.push(e));
  assert.deepEqual(out, [{ event: "log", data: '{"msg":"Größe ✓"}' }]);
});
