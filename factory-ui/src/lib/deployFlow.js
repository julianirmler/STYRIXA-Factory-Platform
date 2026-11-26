// Ablauf eines Deployments als reine Zustandsfunktion:
//   start → (log)* → identity → confirm → (log)* → result | error
// Der Server meldet unbekannte/geänderte Server-Identitäten mit `identity`;
// nach Bestätigung sendet das UI dieselbe Anfrage mit `fingerprint` erneut.

export const initialDeploy = {
  phase: "idle", // idle | running | confirm | done | error
  logs: [],
  identity: null, // {kind, fingerprint, mismatch}
  result: null,
  error: null,
};

const now = () => new Date().toLocaleTimeString("de-AT");
const withLog = (s, level, msg) => [...s.logs, { level, msg, t: now() }];

export function deployReducer(s, a) {
  switch (a.type) {
    case "start":
      return { ...initialDeploy, phase: "running", logs: withLog(initialDeploy, "info", "Paket wird geladen und geprüft …") };
    case "log":
      if (s.phase !== "running") return s;
      return { ...s, logs: withLog(s, a.level, a.msg) };
    case "identity":
      if (s.phase !== "running") return s;
      return { ...s, phase: "confirm", identity: a.identity };
    case "confirm":
      if (s.phase !== "confirm") return s;
      // Geänderte Identität nur nach ausdrücklicher zweiter Bestätigung
      if (s.identity.mismatch && !a.acknowledged) return s;
      return { ...s, phase: "running", identity: null };
    case "cancel":
      if (s.phase !== "confirm") return s;
      return {
        ...s,
        phase: "idle",
        identity: null,
        logs: withLog(s, "warn", "Deployment abgebrochen – Fingerprint nicht bestätigt"),
      };
    case "result":
      if (s.phase !== "running") return s;
      return { ...s, phase: "done", result: a.result };
    case "error":
      if (s.phase !== "running") return s;
      return { ...s, phase: "error", error: a.message, logs: withLog(s, "error", a.message) };
    case "end":
      // Stream ohne abschließendes Ereignis beendet
      if (s.phase !== "running") return s;
      return deployReducer(s, { type: "error", message: "Verbindung zur Factory unterbrochen" });
    case "reset":
      return initialDeploy;
    default:
      return s;
  }
}

/** Schlüssel für bestätigte Fingerprints (nur im Speicher, je Ziel). */
export const pinKey = (method, host, port) => `${method}:${host.toLowerCase()}:${Number(port) || 0}`;

/** Prüft die Eingaben; liefert deutsche Hinweise (leer = gültig). */
export function validateDeploy(method, form, target) {
  if (method === "download") return [];
  const e = [];
  if (!/^[A-Za-z0-9.-]+$/.test(form.host)) e.push("Host ohne Protokoll angeben, z. B. ftp.kunde.at");
  if (form.port !== "" && !(Number.isInteger(Number(form.port)) && form.port > 0 && form.port < 65536))
    e.push("Port muss zwischen 1 und 65535 liegen");
  if (!/^[A-Za-z0-9._@+-]+$/.test(form.user)) e.push("Benutzername fehlt");
  const auth = method === "ftp" ? "password" : form.auth;
  if (auth === "password" && !form.password) e.push("Passwort fehlt");
  if (auth === "key" && !form.privateKey.includes("PRIVATE KEY")) e.push("Privater Schlüssel fehlt");
  // gleiche Regel wie der Server (deploy.rePath): Pfade gehen in Shell-/FTP-Befehle
  const dir = (method === "ssh" ? form.remoteDir : form.docroot).trim();
  const dirLabel = method === "ssh" ? "App-Ordner" : "Document-Root";
  if (!/^[A-Za-z0-9._/~+-]*$/.test(dir))
    e.push(`${dirLabel}: nur Buchstaben, Ziffern und . _ / ~ + - erlaubt (keine Leerzeichen oder Umlaute)`);
  else if (dir.split("/").includes(".."))
    e.push(`${dirLabel}: „..“ ist nicht erlaubt`);
  else if (dir.startsWith("-")) e.push(`${dirLabel}: darf nicht mit - beginnen`);
  else if (dir.includes("~") && dir !== "~" && !dir.startsWith("~/")) e.push(`${dirLabel}: ~ nur am Anfang erlaubt`);
  if (method === "ftp" && !/^https?:\/\/[^/]+/.test(form.publicUrl)) e.push("Website-URL für den Health-Check fehlt");
  if (method === "ftp" && !target.startsWith("linux/"))
    e.push("Webspace läuft auf Linux – Zielplattform Linux wählen");
  return e;
}

/** Anfrage-Body für POST /api/packages/{id}/deploy. */
export function deployBody(method, form, fingerprint) {
  const publicUrl = form.publicUrl === "https://" ? "" : form.publicUrl.trim();
  const base = {
    method,
    host: form.host.trim(),
    port: Number(form.port) || 0,
    user: form.user,
    publicUrl,
    fingerprint: fingerprint || "",
  };
  const target =
    method === "ssh"
      ? { ...base, remoteDir: form.remoteDir.trim(), appPort: Number(form.appPort) || 8080 }
      : { ...base, remoteDir: form.docroot.trim(), tls: form.tls };
  const auth = method === "ftp" ? "password" : form.auth;
  return {
    target,
    password: auth === "password" ? form.password : "",
    privateKey: auth === "key" ? form.privateKey : "",
  };
}
