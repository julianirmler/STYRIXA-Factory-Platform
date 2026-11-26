import { test } from "node:test";
import assert from "node:assert/strict";
import { deployBody, deployReducer, initialDeploy, pinKey, validateDeploy } from "./deployFlow.js";

const run = (...actions) => actions.reduce(deployReducer, initialDeploy);
const ID = { kind: "ssh-host-key", fingerprint: "SHA256:abc", mismatch: false };

test("identity → confirm → result", () => {
  let s = run({ type: "start" }, { type: "log", level: "info", msg: "Verbinde" }, { type: "identity", identity: ID });
  assert.equal(s.phase, "confirm");
  assert.deepEqual(s.identity, ID);
  s = deployReducer(s, { type: "confirm" });
  assert.equal(s.phase, "running");
  assert.equal(s.identity, null);
  assert.equal(s.logs.length, 2, "Log bleibt über die Bestätigung erhalten");
  s = deployReducer(s, { type: "result", result: { release: "r1", installCode: "X" } });
  assert.equal(s.phase, "done");
  assert.equal(s.result.release, "r1");
});

test("identity → confirm → error", () => {
  const s = run({ type: "start" }, { type: "identity", identity: ID }, { type: "confirm" }, { type: "error", message: "Login fehlgeschlagen" });
  assert.equal(s.phase, "error");
  assert.equal(s.error, "Login fehlgeschlagen");
  assert.equal(s.logs.at(-1).level, "error");
});

test("geänderte Identität braucht ausdrückliche Bestätigung", () => {
  const changed = { ...ID, mismatch: true };
  let s = run({ type: "start" }, { type: "identity", identity: changed }, { type: "confirm" });
  assert.equal(s.phase, "confirm", "einfacher Klick reicht nicht");
  s = deployReducer(s, { type: "confirm", acknowledged: true });
  assert.equal(s.phase, "running");
});

test("Abbruch im Dialog", () => {
  const s = run({ type: "start" }, { type: "identity", identity: ID }, { type: "cancel" });
  assert.equal(s.phase, "idle");
  assert.match(s.logs.at(-1).msg, /abgebrochen/);
});

test("Stream ohne Abschluss wird zum Fehler, nach Abschluss ignoriert", () => {
  assert.equal(run({ type: "start" }, { type: "end" }).phase, "error");
  assert.equal(run({ type: "start" }, { type: "identity", identity: ID }, { type: "end" }).phase, "confirm");
  assert.equal(run({ type: "start" }, { type: "result", result: {} }, { type: "end" }).phase, "done");
});

test("Ereignisse außerhalb des Ablaufs ändern nichts", () => {
  assert.equal(run({ type: "result", result: {} }), initialDeploy);
  assert.equal(run({ type: "confirm" }), initialDeploy);
  const done = run({ type: "start" }, { type: "result", result: {} });
  assert.equal(deployReducer(done, { type: "error", message: "x" }), done);
  assert.equal(deployReducer(done, { type: "reset" }), initialDeploy);
});

const FORM = {
  host: "server.kunde.at", port: "", user: "deploy", auth: "password", password: "pw", privateKey: "",
  remoteDir: "styrixa", docroot: "/httpdocs", appPort: 8080, publicUrl: "https://", tls: "explicit",
};

test("Validierung", () => {
  assert.deepEqual(validateDeploy("ssh", FORM, "darwin/arm64"), [], "SSH: Plattform prüft der Server");
  assert.deepEqual(validateDeploy("download", {}, "x"), []);
  assert.ok(validateDeploy("ftp", { ...FORM, publicUrl: "https://kunde.at" }, "windows/amd64").some((e) => /Linux/.test(e)));
  assert.deepEqual(validateDeploy("ftp", { ...FORM, publicUrl: "https://kunde.at" }, "linux/arm64"), []);
  assert.ok(validateDeploy("ftp", FORM, "linux/amd64").some((e) => /Website-URL/.test(e)));
  assert.ok(validateDeploy("ssh", { ...FORM, host: "https://x" }, "linux/amd64").length);
  assert.ok(validateDeploy("ssh", { ...FORM, port: "70000" }, "linux/amd64").length);
  assert.ok(validateDeploy("ssh", { ...FORM, auth: "key" }, "linux/amd64").some((e) => /Schlüssel/.test(e)));
  assert.ok(validateDeploy("ssh", { ...FORM, password: "" }, "linux/amd64").some((e) => /Passwort/.test(e)));
});

test("Ordner-Prüfung wie am Server", () => {
  const ftp = { ...FORM, publicUrl: "https://t.kunde.at" };
  assert.deepEqual(validateDeploy("ftp", { ...ftp, docroot: "/html/factory-test" }, "linux/amd64"), []);
  assert.deepEqual(validateDeploy("ftp", { ...ftp, docroot: " /html/x \n" }, "linux/amd64"), [], "Rand-Leerzeichen werden abgeschnitten");
  assert.equal(deployBody("ftp", { ...ftp, docroot: " /html/x " }).target.remoteDir, "/html/x");
  for (const bad of ["/html/factory test", "https://t.kunde.at", "/html/übung", "-x", "/a/~b"]) {
    assert.ok(validateDeploy("ftp", { ...ftp, docroot: bad }, "linux/amd64").some((e) => e.startsWith("Document-Root:")), bad);
  }
  assert.ok(validateDeploy("ftp", { ...ftp, docroot: "/html/../x" }, "linux/amd64").some((e) => /\.\./.test(e)));
  assert.ok(validateDeploy("ssh", { ...FORM, remoteDir: "my app" }, "linux/amd64").some((e) => e.startsWith("App-Ordner:")));
});

test("Anfrage-Body", () => {
  const ssh = deployBody("ssh", { ...FORM, auth: "key", privateKey: "K" }, "SHA256:abc");
  assert.deepEqual(ssh, {
    target: { method: "ssh", host: "server.kunde.at", port: 0, user: "deploy", publicUrl: "", fingerprint: "SHA256:abc", remoteDir: "styrixa", appPort: 8080 },
    password: "",
    privateKey: "K",
  });
  const ftp = deployBody("ftp", { ...FORM, auth: "key", port: "990", tls: "implicit", publicUrl: "https://t.kunde.at" });
  assert.equal(ftp.password, "pw", "FTP nutzt immer Passwort");
  assert.equal(ftp.privateKey, "");
  assert.deepEqual(ftp.target, { method: "ftp", host: "server.kunde.at", port: 990, user: "deploy", publicUrl: "https://t.kunde.at", fingerprint: "", remoteDir: "/httpdocs", tls: "implicit" });
});

test("Pin-Schlüssel je Ziel", () => {
  assert.equal(pinKey("ssh", "Server.Kunde.AT", ""), "ssh:server.kunde.at:0");
  assert.notEqual(pinKey("ssh", "a", 22), pinKey("ftp", "a", 22));
});
