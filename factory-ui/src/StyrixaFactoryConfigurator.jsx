import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  LayoutDashboard,
  FileText,
  Image as ImageIcon,
  Search,
  Users,
  Palette,
  Package,
  Lock,
  Check,
  Cpu,
  Server,
  Monitor,
  Laptop,
  Rocket,
  Terminal,
  Download,
  Loader2,
  Database,
  ShieldCheck,
  Copy,
  RotateCcw,
  Info,
  Hash,
  Boxes,
  ShoppingCart,
  UploadCloud,
  KeyRound,
  Globe,
  AlertTriangle,
  Fingerprint,
  X,
} from "lucide-react";

/* ------------------------------------------------------------------ */
/*  STYRIXA Factory – Configurator UI (Mockup)                         */
/*  React + Tailwind CSS + lucide-react                                 */
/*  Der Build-Ablauf ist hier simuliert. Später kommt er per            */
/*  POST /api/builds + SSE-Log-Stream aus der factory-api.              */
/* ------------------------------------------------------------------ */

const MODULES = [
  {
    id: "dashboard",
    nr: "01",
    tag: "mod_dashboard",
    name: "Dashboard",
    icon: LayoutDashboard,
    required: true,
    sizeKb: 420,
    desc: "Besucher-Statistiken, Systemstatus und Schnellzugriffe.",
    features: ["Besucher-Stats", "Status", "Quick Actions"],
  },
  {
    id: "content",
    nr: "02",
    tag: "mod_content",
    name: "Content Management",
    icon: FileText,
    required: true,
    sizeKb: 1180,
    desc: "Drag-and-Drop-Blöcke, HTML Studio (Raw HTML/CSS), Auto-Save.",
    features: ["Block-Editor", "HTML Studio", "Auto-Save"],
  },
  {
    id: "media",
    nr: "03",
    tag: "mod_media",
    name: "Media Management",
    icon: ImageIcon,
    sizeKb: 1650,
    desc: "WebP/AVIF-Auto-Optimierung, Ordnerstruktur, Alt-Texte.",
    features: ["WebP/AVIF", "Ordner", "Alt-Texte"],
  },
  {
    id: "seo",
    nr: "04",
    tag: "mod_seo",
    name: "SEO & GEO Management",
    icon: Search,
    sizeKb: 540,
    desc: "Meta, Sitemap, robots.txt, 301-Redirects, Schema.org JSON-LD, Answer-First.",
    features: ["Sitemap", "301", "JSON-LD"],
  },
  {
    id: "users",
    nr: "05",
    tag: "mod_users",
    name: "User Management",
    icon: Users,
    sizeKb: 760,
    desc: "RBAC (Admin, Redakteur, Autor, Kunde), 2FA (TOTP), Activity-Log.",
    features: ["RBAC", "2FA", "Activity-Log"],
  },
  {
    id: "design",
    nr: "06",
    tag: "mod_design",
    name: "Design Management",
    icon: Palette,
    sizeKb: 380,
    desc: "Globale Theme-Tokens, Farben, Fonts und Templates.",
    features: ["Tokens", "Fonts", "Templates"],
  },
  {
    id: "themes",
    nr: "07",
    tag: "mod_themes",
    name: "Theme Management",
    icon: Package,
    sizeKb: 610,
    requires: ["design"],
    desc: "ZIP-Upload (archive/zip), theme.json-Validierung, Entpacken nach ./themes/<id>/, 1-Klick-Aktivierung.",
    features: ["ZIP-Upload", "theme.json", "1-Klick"],
  },
  {
    id: "shop",
    nr: "08",
    tag: "mod_shop",
    name: "Shop Management",
    icon: ShoppingCart,
    sizeKb: 1420,
    requires: ["media"],
    desc: "Produkte & Varianten, Lager, Bestellungen, DACH-Steuern, Versand, Gutscheine, Rechnungen, Product-Schema.org.",
    features: ["Produkte", "Bestellungen", "USt AT/DE/CH", "Stripe/EPS", "Rechnungen"],
  },
];

const TARGETS = [
  { id: "linux-amd64", goos: "linux", goarch: "amd64", label: "Linux", sub: "x86_64 · Server/VPS", icon: Server },
  { id: "linux-arm64", goos: "linux", goarch: "arm64", label: "Linux ARM", sub: "arm64 · Graviton/Pi", icon: Cpu },
  { id: "darwin-arm64", goos: "darwin", goarch: "arm64", label: "macOS", sub: "Apple Silicon", icon: Laptop },
  { id: "windows-amd64", goos: "windows", goarch: "amd64", label: "Windows", sub: "x86_64 · .exe", icon: Monitor },
];

// Auslieferungswege nach dem Build
const DELIVERY = [
  {
    id: "download",
    label: "Download",
    sub: ".tar.gz selbst installieren",
    icon: Download,
  },
  {
    id: "ssh",
    label: "Server per SSH",
    sub: "VPS, Managed Server, Hosting mit SSH",
    icon: Server,
    badge: "Empfohlen",
  },
  {
    id: "ftp",
    label: "Webspace per FTP(S)",
    sub: "Shared Hosting mit Apache (CGI-Modus)",
    icon: Globe,
  },
];

// true = Deploy wird im Browser simuliert; false = echte factory-api (SSE)
const MOCK_DEPLOY = true;

const BASE_SIZE_KB = 7800; // Go-Runtime + net/http + modernc.org/sqlite + Installer

const BUILD_STEPS = [
  { key: "validate", label: "Config validieren" },
  { key: "workspace", label: "Workspace vorbereiten" },
  { key: "deps", label: "Module & Abhängigkeiten auflösen" },
  { key: "compile", label: "go build (CGO_ENABLED=0)" },
  { key: "package", label: "tar.gz paketieren" },
  { key: "checksum", label: "SHA-256 & Signatur" },
];

function fmtSize(kb) {
  return kb >= 1024 ? `${(kb / 1024).toFixed(1)} MB` : `${kb} KB`;
}

function randHex(n) {
  const c = "0123456789abcdef";
  let s = "";
  for (let i = 0; i < n; i++) s += c[Math.floor(Math.random() * 16)];
  return s;
}

/* ------------------------------ Logo ------------------------------ */
function StyrixaMark() {
  return (
    <div className="relative h-9 w-9">
      <div className="absolute inset-0 rotate-45 rounded-md bg-gradient-to-br from-cyan-400 to-emerald-400" />
      <div className="absolute inset-[5px] rotate-45 rounded-[4px] bg-slate-950" />
      <span className="absolute inset-0 flex items-center justify-center text-sm font-black tracking-tight text-cyan-300">
        S
      </span>
    </div>
  );
}

/* --------------------------- Hauptkomponente --------------------------- */
export default function StyrixaFactoryConfigurator() {
  const [selected, setSelected] = useState(
    () => new Set(MODULES.filter((m) => m.required || ["seo", "media"].includes(m.id)).map((m) => m.id))
  );
  const [target, setTarget] = useState("linux-amd64");
  const [siteName, setSiteName] = useState("styrixa-cms");
  const [region, setRegion] = useState("de-AT");
  const [notice, setNotice] = useState(null);

  // Auslieferung / Deploy
  const [delivery, setDelivery] = useState("download");
  const [dep, setDep] = useState({
    host: "",
    port: "",
    user: "",
    auth: "password", // password | key
    password: "",
    privateKey: "",
    remoteDir: "styrixa",
    docroot: "/httpdocs",
    appPort: 8080,
    publicUrl: "https://",
    tls: "explicit",
  });
  const [deployPhase, setDeployPhase] = useState("idle"); // idle | running | confirm | done | error
  const [deployLogs, setDeployLogs] = useState([]);
  const [identity, setIdentity] = useState(null); // {kind, fingerprint, mismatch}
  const [confirmedFp, setConfirmedFp] = useState("");
  const [deployResult, setDeployResult] = useState(null);
  const deployTimers = useRef([]);

  // Build-State
  const [phase, setPhase] = useState("idle"); // idle | running | done
  const [stepIdx, setStepIdx] = useState(-1);
  const [logs, setLogs] = useState([]);
  const [result, setResult] = useState(null);
  const [copied, setCopied] = useState(false);
  const logRef = useRef(null);
  const timers = useRef([]);

  const tgt = TARGETS.find((t) => t.id === target);
  const activeModules = MODULES.filter((m) => selected.has(m.id));
  const tags = activeModules.map((m) => m.tag);
  const estKb = BASE_SIZE_KB + activeModules.reduce((a, m) => a + m.sizeKb, 0);
  const binName = `styrixa${tgt.goos === "windows" ? ".exe" : ""}`;
  const slug = siteName.trim().toLowerCase().replace(/[^a-z0-9-]+/g, "-") || "styrixa-cms";
  const artifact = `${slug}_${tgt.goos}_${tgt.goarch}.tar.gz`;

  const buildCmd = useMemo(
    () =>
      `CGO_ENABLED=0 GOOS=${tgt.goos} GOARCH=${tgt.goarch} \\\n` +
      `  go build -trimpath -ldflags "-s -w" \\\n` +
      `  -tags "${tags.join(" ")}" \\\n` +
      `  -o dist/${binName} ./cmd/styrixa`,
    [tgt, tags.join(","), binName]
  );

  const buildConfig = useMemo(
    () => ({
      name: slug,
      target: { goos: tgt.goos, goarch: tgt.goarch },
      modules: activeModules.map((m) => m.id),
      defaults: { region },
    }),
    [slug, tgt, activeModules.length, region]
  );

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [logs]);

  useEffect(() => () => timers.current.forEach(clearTimeout), []);

  const locked = phase === "running";

  function toggle(mod) {
    if (mod.required || locked) return;
    const next = new Set(selected);
    if (next.has(mod.id)) {
      // Abhängige Module mit abwählen
      const dependents = MODULES.filter((m) => m.requires?.includes(mod.id) && next.has(m.id));
      next.delete(mod.id);
      dependents.forEach((d) => next.delete(d.id));
      if (dependents.length)
        setNotice(`${dependents.map((d) => d.name).join(", ")} wurde mit abgewählt (benötigt ${mod.name}).`);
      else setNotice(null);
    } else {
      next.add(mod.id);
      const missing = (mod.requires || []).filter((r) => !next.has(r));
      missing.forEach((r) => next.add(r));
      if (missing.length)
        setNotice(
          `${missing.map((r) => MODULES.find((m) => m.id === r).name).join(", ")} automatisch aktiviert (benötigt von ${mod.name}).`
        );
      else setNotice(null);
    }
    setSelected(next);
    if (phase === "done") resetBuild();
  }

  function selectAll() {
    if (locked) return;
    setSelected(new Set(MODULES.map((m) => m.id)));
    setNotice(null);
    if (phase === "done") resetBuild();
  }

  function selectMinimal() {
    if (locked) return;
    setSelected(new Set(MODULES.filter((m) => m.required).map((m) => m.id)));
    setNotice(null);
    if (phase === "done") resetBuild();
  }

  function resetDeploy() {
    deployTimers.current.forEach(clearTimeout);
    deployTimers.current = [];
    setDeployPhase("idle");
    setDeployLogs([]);
    setIdentity(null);
    setDeployResult(null);
  }

  function resetBuild() {
    resetDeploy();
    timers.current.forEach(clearTimeout);
    timers.current = [];
    setPhase("idle");
    setStepIdx(-1);
    setLogs([]);
    setResult(null);
  }

  function startBuild() {
    resetBuild();
    setPhase("running");
    const id = `bld_${randHex(8)}`;
    const t0 = new Date();
    const script = [
      [0, 300, `▸ POST /api/builds  →  ${id}`],
      [0, 250, `  config: ${JSON.stringify(buildConfig)}`],
      [0, 350, `  ✓ Schema ok · ${tags.length} Module · Ziel ${tgt.goos}/${tgt.goarch}`],
      [1, 400, `▸ workspace: /var/factory/jobs/${id}`],
      [1, 300, `  cp -r cms-template/ → src/ (read-only snapshot)`],
      [1, 300, `  GOFLAGS=-mod=vendor · GOCACHE shared`],
      [2, 350, `▸ build-tags: ${tags.join(" ")}`],
      ...activeModules
        .filter((m) => m.requires)
        .map((m) => [2, 250, `  dep: ${m.tag} → ${m.requires.map((r) => "mod_" + r).join(", ")} ✓`]),
      [2, 300, `  ausgeschlossen: ${MODULES.filter((m) => !selected.has(m.id)).map((m) => m.tag).join(" ") || "—"}`],
      [3, 300, `▸ CGO_ENABLED=0 GOOS=${tgt.goos} GOARCH=${tgt.goarch}`],
      [3, 450, `  compiling modernc.org/sqlite (pure Go) …`],
      [3, 400, `  compiling internal/installer (/install wizard) …`],
      ...activeModules.map((m) => [3, 280, `  compiling internal/modules/${m.id} …`]),
      [3, 500, `  embed: web/admin, web/install, migrations/*.sql`],
      [3, 300, `  ✓ dist/${binName} (${fmtSize(estKb)})`],
      [4, 400, `▸ tar -czf ${artifact}`],
      [4, 250, `  + ${binName}  + README.md  + LICENSE  + themes/.keep`],
      [5, 350, `▸ sha256: ${randHex(64)}`],
      [5, 300, `  ✓ Build abgeschlossen in ${(4 + activeModules.length * 0.6).toFixed(1)} s`],
    ];

    let acc = 0;
    script.forEach(([step, delay, line], i) => {
      acc += delay;
      timers.current.push(
        setTimeout(() => {
          setStepIdx(step);
          setLogs((l) => [...l, line]);
          if (i === script.length - 1) {
            setStepIdx(BUILD_STEPS.length);
            setPhase("done");
            setResult({
              id,
              file: artifact,
              size: fmtSize(Math.round(estKb * 0.42)),
              bin: fmtSize(estKb),
              at: t0.toLocaleTimeString("de-AT"),
            });
          }
        }, acc)
      );
    });
  }

  const setD = (k) => (e) => setDep((d) => ({ ...d, [k]: e.target.value }));
  const depLocked = deployPhase === "running";

  const deployErrors = useMemo(() => {
    if (delivery === "download") return [];
    const e = [];
    if (!/^[A-Za-z0-9.-]+$/.test(dep.host)) e.push("Host ohne Protokoll angeben, z. B. ftp.kunde.at");
    if (!/^[A-Za-z0-9._@+-]+$/.test(dep.user)) e.push("Benutzername fehlt");
    if (dep.auth === "password" && !dep.password) e.push("Passwort fehlt");
    if (delivery === "ssh" && dep.auth === "key" && !dep.privateKey.includes("PRIVATE KEY")) e.push("Privater Schlüssel fehlt");
    if (delivery === "ftp" && !/^https?:\/\/[^/]+/.test(dep.publicUrl)) e.push("Website-URL für den Health-Check fehlt");
    if (delivery === "ftp" && tgt.goos !== "linux") e.push("Webspace läuft auf Linux – Zielplattform Linux wählen");
    return e;
  }, [delivery, dep, tgt]);

  const pushLog = (level, msg) =>
    setDeployLogs((l) => [...l, { level, msg, t: new Date().toLocaleTimeString("de-AT") }]);

  function deployTarget(fp) {
    const base = {
      method: delivery,
      host: dep.host,
      port: Number(dep.port) || 0,
      user: dep.user,
      publicUrl: dep.publicUrl === "https://" ? "" : dep.publicUrl,
      fingerprint: fp,
    };
    return delivery === "ssh"
      ? { ...base, remoteDir: dep.remoteDir, appPort: Number(dep.appPort) || 8080 }
      : { ...base, remoteDir: dep.docroot, tls: dep.tls };
  }

  // Echte Anbindung: POST /api/builds/{id}/deploy → Server-Sent Events
  async function streamDeploy(fp) {
    const res = await fetch(`/api/builds/${result.id}/deploy`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        target: deployTarget(fp),
        password: dep.auth === "password" ? dep.password : "",
        privateKey: dep.auth === "key" ? dep.privateKey : "",
      }),
    });
    if (!res.ok) throw new Error(await res.text());
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let buf = "";
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += dec.decode(value, { stream: true });
      let i;
      while ((i = buf.indexOf("\n\n")) >= 0) {
        const chunk = buf.slice(0, i);
        buf = buf.slice(i + 2);
        const ev = /^event: (.*)$/m.exec(chunk)?.[1];
        const data = JSON.parse(/^data: (.*)$/m.exec(chunk)?.[1] || "{}");
        if (ev === "log") pushLog(data.level, data.msg);
        if (ev === "identity") { setIdentity(data); setDeployPhase("confirm"); }
        if (ev === "result") { setDeployResult(data); setDeployPhase("done"); }
        if (ev === "error") { pushLog("error", data.message); setDeployPhase("error"); }
      }
    }
  }

  // Simulation für den Mockup
  function mockDeploy(fp) {
    const isSSH = delivery === "ssh";
    const fakeFp = isSSH ? "SHA256:q7nF2c9xVb1kPz0R8mWlT4aYd6sHgE3uJiK5oNpQrS0" : "SHA256:Zk3P8sWq1LmN7bVx2cR5tY9uI0oPaS4dF6gH8jK1lE0";
    const kind = isSSH ? "ssh-host-key" : "tls-certificate";
    const steps = [[300, "info", `Verbinde mit ${dep.host}${isSSH ? ":" + (dep.port || 22) : ":" + (dep.port || 21) + " (FTPS)"} als ${dep.user} …`]];
    if (!fp) {
      steps.forEach(([d, l, m]) => deployTimers.current.push(setTimeout(() => pushLog(l, m), d)));
      deployTimers.current.push(setTimeout(() => {
        setIdentity({ kind, fingerprint: fakeFp, mismatch: false });
        setDeployPhase("confirm");
      }, 900));
      return;
    }
    const rel = new Date().toISOString().replace(/[-:]/g, "").slice(0, 15) + "Z";
    const more = isSSH
      ? [
          [400, "ok", `Verbunden, Host-Key ${fp.slice(0, 18)}… bestätigt`],
          [400, "ok", `Server-Plattform ${tgt.goos}/${tgt.goarch} passt`],
          [700, "info", `Lade Binary hoch (${fmtSize(estKb)}) …`],
          [900, "ok", `Release ${rel} hochgeladen`],
          [300, "ok", `current → releases/${rel}`],
          [500, "ok", `systemd-User-Service ${"styrixa-" + slug} gestartet`],
          [700, "info", `Health-Check 127.0.0.1:${dep.appPort}/healthz …`],
          [600, "ok", "App antwortet"],
        ]
      : [
          [400, "ok", `Angemeldet als ${dep.user} (FTPS, Zertifikat gepinnt)`],
          [300, "ok", "Datenordner styrixa-data/ angelegt und geschützt"],
          [900, "info", `Lade styrixa.cgi hoch (${fmtSize(estKb)}) …`],
          [500, "ok", "styrixa.cgi aktiv (Rechte 755)"],
          [400, "warn", `Bestehende .htaccess gesichert als .htaccess.styrixa-backup-${rel}`],
          [300, "ok", "CGI-Routing eingerichtet"],
          [700, "info", `Health-Check ${dep.publicUrl.replace(/\/$/, "")}/healthz …`],
          [600, "ok", "Website antwortet"],
        ];
    let acc = 0;
    [...steps, ...more].forEach(([d, l, m]) => {
      acc += d;
      deployTimers.current.push(setTimeout(() => pushLog(l, m), acc));
    });
    deployTimers.current.push(
      setTimeout(() => {
        setDeployResult({
          release: rel,
          url: dep.publicUrl === "https://" ? "" : dep.publicUrl,
          installCode: "DEMO-CODE-NURZ-UMTE",
          hints: isSSH
            ? [`App lauscht auf 127.0.0.1:${dep.appPort} – Domain per Caddy/nginx darauf zeigen lassen.`]
            : ["CGI-Modus: ideal für kleine bis mittlere Websites."],
          caddy: isSSH ? `${(dep.publicUrl.replace(/^https?:\/\//, "").replace(/\/.*$/, "")) || "kunde.at"} {\n\treverse_proxy 127.0.0.1:${dep.appPort}\n}` : null,
        });
        setDeployPhase("done");
      }, acc + 300)
    );
  }

  function startDeploy(fp = confirmedFp) {
    resetDeploy();
    setDeployPhase("running");
    if (MOCK_DEPLOY) return mockDeploy(fp);
    streamDeploy(fp).catch((e) => {
      pushLog("error", e.message);
      setDeployPhase("error");
    });
  }

  function confirmIdentity() {
    const fp = identity.fingerprint;
    setConfirmedFp(fp);
    setIdentity(null);
    startDeploy(fp);
  }

  function copyCmd() {
    try {
      navigator.clipboard.writeText(buildCmd.replace(/\\\n\s*/g, ""));
      setCopied(true);
      setTimeout(() => setCopied(false), 1400);
    } catch (_) {
      /* clipboard nicht verfügbar */
    }
  }

  const progress =
    phase === "idle" ? 0 : Math.min(100, Math.round(((stepIdx + (phase === "done" ? 0 : 0.5)) / BUILD_STEPS.length) * 100));

  return (
    <div className="min-h-screen bg-slate-950 text-slate-200 antialiased selection:bg-cyan-400/30">
      {/* Hintergrund-Raster */}
      <div
        className="pointer-events-none fixed inset-0 opacity-[0.07]"
        style={{
          backgroundImage:
            "linear-gradient(rgba(148,163,184,.6) 1px, transparent 1px), linear-gradient(90deg, rgba(148,163,184,.6) 1px, transparent 1px)",
          backgroundSize: "32px 32px",
        }}
      />
      <div className="pointer-events-none fixed -top-40 left-1/3 h-96 w-96 rounded-full bg-cyan-500/20 blur-3xl" />

      {/* Header */}
      <header className="relative border-b border-slate-800/80 bg-slate-950/70 backdrop-blur">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-6 py-4">
          <div className="flex items-center gap-3">
            <StyrixaMark />
            <div className="leading-tight">
              <div className="text-lg font-black tracking-[0.18em] text-white">STYRIXA</div>
              <div className="text-[10px] uppercase tracking-[0.28em] text-slate-500">Digital Technology Studio</div>
            </div>
            <span className="ml-4 hidden rounded-full border border-cyan-500/30 bg-cyan-500/10 px-3 py-1 text-xs font-medium text-cyan-300 sm:inline">
              Factory Platform
            </span>
          </div>
          <div className="flex items-center gap-4 text-xs text-slate-500">
            <span className="hidden items-center gap-1.5 md:flex">
              <span className="h-2 w-2 rounded-full bg-emerald-400 shadow-[0_0_8px] shadow-emerald-400" />
              Build-Worker online
            </span>
            <span className="font-mono">go1.23 · cms v0.1.0</span>
          </div>
        </div>
      </header>

      <main className="relative mx-auto grid max-w-7xl gap-8 px-6 py-10 lg:grid-cols-[1fr_400px]">
        {/* ------------------------- Linke Spalte ------------------------- */}
        <section>
          <div className="mb-8">
            <p className="mb-2 font-mono text-xs uppercase tracking-[0.25em] text-cyan-400">Schritt 1 · Module</p>
            <h1 className="text-3xl font-bold tracking-tight text-white md:text-4xl">
              Dein STYRIXA CMS, als ein einziges Binary.
            </h1>
            <p className="mt-3 max-w-2xl text-slate-400">
              Wähle die Module. Die Factory kompiliert daraus ein CGO-freies Go-Binary mit eingebetteter SQLite
              und 1-Klick-Installer. Nicht gewählte Module werden per Build-Tag gar nicht erst mitkompiliert.
            </p>
          </div>

          <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
            <div className="text-sm text-slate-400">
              <span className="font-semibold text-white">{activeModules.length}</span> / {MODULES.length} Module aktiv
            </div>
            <div className="flex gap-2">
              <button
                onClick={selectMinimal}
                disabled={locked}
                className="rounded-lg border border-slate-700 px-3 py-1.5 text-xs text-slate-300 transition hover:border-slate-500 disabled:opacity-40"
              >
                Minimal
              </button>
              <button
                onClick={selectAll}
                disabled={locked}
                className="rounded-lg border border-slate-700 px-3 py-1.5 text-xs text-slate-300 transition hover:border-slate-500 disabled:opacity-40"
              >
                Alle Module
              </button>
            </div>
          </div>

          {notice && (
            <div className="mb-4 flex items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-200">
              <Info className="mt-0.5 h-4 w-4 shrink-0" />
              {notice}
            </div>
          )}

          <div className="grid gap-4 sm:grid-cols-2">
            {MODULES.map((m) => {
              const on = selected.has(m.id);
              const Icon = m.icon;
              return (
                <button
                  key={m.id}
                  onClick={() => toggle(m)}
                  disabled={locked}
                  aria-pressed={on}
                  className={[
                    "group relative overflow-hidden rounded-2xl border p-5 text-left transition",
                    on
                      ? "border-cyan-400/50 bg-gradient-to-br from-cyan-500/10 to-emerald-500/5 shadow-[0_0_0_1px_rgba(34,211,238,.15),0_10px_40px_-15px_rgba(34,211,238,.4)]"
                      : "border-slate-800 bg-slate-900/50 hover:border-slate-600",
                    m.required ? "cursor-default" : "cursor-pointer",
                    locked ? "opacity-70" : "",
                  ].join(" ")}
                >
                  <div className="mb-4 flex items-start justify-between">
                    <div className="flex items-center gap-3">
                      <div
                        className={[
                          "flex h-11 w-11 items-center justify-center rounded-xl border transition",
                          on ? "border-cyan-400/40 bg-cyan-400/15 text-cyan-300" : "border-slate-700 bg-slate-800/60 text-slate-400",
                        ].join(" ")}
                      >
                        <Icon className="h-5 w-5" />
                      </div>
                      <div>
                        <div className="font-mono text-[11px] text-slate-500">{m.nr}</div>
                        <div className="font-semibold text-white">{m.name}</div>
                      </div>
                    </div>
                    <div
                      className={[
                        "flex h-6 w-6 items-center justify-center rounded-md border transition",
                        m.required
                          ? "border-slate-600 bg-slate-700 text-slate-300"
                          : on
                          ? "border-cyan-400 bg-cyan-400 text-slate-950"
                          : "border-slate-600 text-transparent group-hover:border-slate-400",
                      ].join(" ")}
                    >
                      {m.required ? <Lock className="h-3.5 w-3.5" /> : <Check className="h-4 w-4" strokeWidth={3} />}
                    </div>
                  </div>

                  <p className="mb-4 text-sm leading-relaxed text-slate-400">{m.desc}</p>

                  <div className="flex flex-wrap items-center gap-1.5">
                    {m.features.map((f) => (
                      <span key={f} className="rounded-md bg-slate-800/80 px-2 py-0.5 text-[11px] text-slate-300">
                        {f}
                      </span>
                    ))}
                  </div>

                  <div className="mt-4 flex items-center justify-between border-t border-slate-800 pt-3 font-mono text-[11px]">
                    <span className={on ? "text-cyan-300" : "text-slate-500"}>{m.tag}</span>
                    <span className="text-slate-500">
                      {m.required ? "Pflicht" : `+${fmtSize(m.sizeKb)}`}
                      {m.requires && <span className="ml-2 text-amber-300/80">↳ mod_{m.requires.join(", mod_")}</span>}
                    </span>
                  </div>
                </button>
              );
            })}

            {/* Kern-Karte */}
            <div className="rounded-2xl border border-dashed border-slate-700 bg-slate-900/30 p-5">
              <div className="mb-3 flex items-center gap-2 text-sm font-semibold text-white">
                <Boxes className="h-4 w-4 text-emerald-400" /> Immer enthalten: Kern
              </div>
              <ul className="space-y-2 text-sm text-slate-400">
                <li className="flex items-center gap-2">
                  <Database className="h-4 w-4 text-slate-500" /> styrixa.db · modernc.org/sqlite
                </li>
                <li className="flex items-center gap-2">
                  <Rocket className="h-4 w-4 text-slate-500" /> /install Web-Installer
                </li>
                <li className="flex items-center gap-2">
                  <ShieldCheck className="h-4 w-4 text-slate-500" /> Admin-Login (bcrypt)
                </li>
                <li className="flex items-center gap-2">
                  <Hash className="h-4 w-4 text-slate-500" /> DACH-Locale · GISA-Feld
                </li>
              </ul>
            </div>
          </div>

          {/* Ziel-OS */}
          <div className="mt-12">
            <p className="mb-2 font-mono text-xs uppercase tracking-[0.25em] text-cyan-400">Schritt 2 · Zielplattform</p>
            <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
              {TARGETS.map((t) => {
                const on = t.id === target;
                const Icon = t.icon;
                return (
                  <button
                    key={t.id}
                    disabled={locked}
                    onClick={() => {
                      setTarget(t.id);
                      if (phase === "done") resetBuild();
                    }}
                    className={[
                      "rounded-xl border p-4 text-left transition disabled:opacity-60",
                      on ? "border-cyan-400/60 bg-cyan-500/10" : "border-slate-800 bg-slate-900/50 hover:border-slate-600",
                    ].join(" ")}
                  >
                    <Icon className={`mb-3 h-5 w-5 ${on ? "text-cyan-300" : "text-slate-500"}`} />
                    <div className="text-sm font-semibold text-white">{t.label}</div>
                    <div className="text-xs text-slate-500">{t.sub}</div>
                    <div className="mt-2 font-mono text-[10px] text-slate-600">
                      {t.goos}/{t.goarch}
                    </div>
                  </button>
                );
              })}
            </div>
          </div>

          {/* Defaults */}
          <div className="mt-12">
            <p className="mb-2 font-mono text-xs uppercase tracking-[0.25em] text-cyan-400">Schritt 3 · Paket</p>
            <div className="grid gap-4 md:grid-cols-2">
              <label className="block">
                <span className="mb-1.5 block text-xs text-slate-400">Paketname</span>
                <input
                  value={siteName}
                  disabled={locked}
                  onChange={(e) => setSiteName(e.target.value)}
                  className="w-full rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 font-mono text-sm text-white outline-none focus:border-cyan-400"
                />
              </label>
              <label className="block">
                <span className="mb-1.5 block text-xs text-slate-400">Vorausgewählte Region im Installer</span>
                <select
                  value={region}
                  disabled={locked}
                  onChange={(e) => setRegion(e.target.value)}
                  className="w-full rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-white outline-none focus:border-cyan-400"
                >
                  <option value="de-AT">Österreich (de-AT)</option>
                  <option value="de-DE">Deutschland (de-DE)</option>
                  <option value="de-CH">Schweiz (de-CH)</option>
                </select>
              </label>
            </div>
            <p className="mt-2 text-xs text-slate-500">
              Seitentitel, GISA-Nummer und Admin-Account werden erst beim ersten Start unter <span className="font-mono">/install</span> gesetzt.
            </p>
          </div>

          {/* Auslieferung */}
          <div className="mt-12">
            <p className="mb-2 font-mono text-xs uppercase tracking-[0.25em] text-cyan-400">Schritt 4 · Auslieferung</p>
            <div className="grid gap-3 md:grid-cols-3">
              {DELIVERY.map((d) => {
                const on = d.id === delivery;
                const Icon = d.icon;
                return (
                  <button
                    key={d.id}
                    disabled={locked || depLocked}
                    onClick={() => {
                      setDelivery(d.id);
                      setConfirmedFp("");
                      resetDeploy();
                      setDep((x) => ({ ...x, port: "", auth: d.id === "ftp" ? "password" : x.auth }));
                    }}
                    className={[
                      "relative rounded-xl border p-4 text-left transition disabled:opacity-60",
                      on ? "border-cyan-400/60 bg-cyan-500/10" : "border-slate-800 bg-slate-900/50 hover:border-slate-600",
                    ].join(" ")}
                  >
                    {d.badge && (
                      <span className="absolute right-3 top-3 rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-300">
                        {d.badge}
                      </span>
                    )}
                    <Icon className={`mb-3 h-5 w-5 ${on ? "text-cyan-300" : "text-slate-500"}`} />
                    <div className="text-sm font-semibold text-white">{d.label}</div>
                    <div className="text-xs text-slate-500">{d.sub}</div>
                  </button>
                );
              })}
            </div>

            {delivery !== "download" && (
              <div className="mt-4 rounded-2xl border border-slate-800 bg-slate-900/50 p-5">
                <div className="grid gap-4 md:grid-cols-[1fr_110px]">
                  <Field label={delivery === "ssh" ? "SSH-Host" : "FTP-Host"}>
                    <input value={dep.host} onChange={setD("host")} disabled={depLocked}
                      placeholder={delivery === "ssh" ? "server.kunde.at" : "ftp.kunde.at"} className={inputCls + " font-mono"} />
                  </Field>
                  <Field label="Port">
                    <input value={dep.port} onChange={setD("port")} disabled={depLocked}
                      placeholder={delivery === "ssh" ? "22" : dep.tls === "implicit" ? "990" : "21"} className={inputCls + " font-mono"} />
                  </Field>
                </div>
                <div className="mt-4 grid gap-4 md:grid-cols-2">
                  <Field label="Benutzer">
                    <input value={dep.user} onChange={setD("user")} disabled={depLocked} autoComplete="off" className={inputCls + " font-mono"} />
                  </Field>
                  {delivery === "ssh" ? (
                    <Field label="Anmeldung">
                      <div className="flex rounded-lg border border-slate-700 p-0.5 text-xs">
                        {[["password", "Passwort"], ["key", "Deploy-Key"]].map(([k, l]) => (
                          <button key={k} type="button" disabled={depLocked}
                            onClick={() => setDep((d) => ({ ...d, auth: k }))}
                            className={`flex-1 rounded-md px-2 py-1.5 ${dep.auth === k ? "bg-slate-700 text-white" : "text-slate-400"}`}>
                            {l}
                          </button>
                        ))}
                      </div>
                    </Field>
                  ) : (
                    <Field label="Verschlüsselung">
                      <select value={dep.tls} onChange={setD("tls")} disabled={depLocked} className={inputCls}>
                        <option value="explicit">FTPS – explizites TLS (Port 21)</option>
                        <option value="implicit">FTPS – implizites TLS (Port 990)</option>
                      </select>
                    </Field>
                  )}
                </div>

                {dep.auth === "key" && delivery === "ssh" ? (
                  <Field label="Privater SSH-Schlüssel (ohne Passphrase)" className="mt-4">
                    <textarea value={dep.privateKey} onChange={setD("privateKey")} disabled={depLocked} rows={3}
                      placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" className={inputCls + " font-mono text-[11px]"} />
                  </Field>
                ) : (
                  <Field label="Passwort" className="mt-4">
                    <input type="password" value={dep.password} onChange={setD("password")} disabled={depLocked}
                      autoComplete="new-password" className={inputCls} />
                  </Field>
                )}

                <div className="mt-4 grid gap-4 md:grid-cols-2">
                  {delivery === "ssh" ? (
                    <>
                      <Field label="App-Ordner (relativ zu ~)">
                        <input value={dep.remoteDir} onChange={setD("remoteDir")} disabled={depLocked} className={inputCls + " font-mono"} />
                      </Field>
                      <Field label="Interner App-Port">
                        <input value={dep.appPort} onChange={setD("appPort")} disabled={depLocked} className={inputCls + " font-mono"} />
                      </Field>
                    </>
                  ) : (
                    <Field label="Document-Root">
                      <input value={dep.docroot} onChange={setD("docroot")} disabled={depLocked} className={inputCls + " font-mono"} />
                    </Field>
                  )}
                  <Field label={delivery === "ftp" ? "Website-URL (für Health-Check)" : "Domain (optional)"}>
                    <input value={dep.publicUrl} onChange={setD("publicUrl")} disabled={depLocked} className={inputCls + " font-mono"} />
                  </Field>
                </div>

                <div className="mt-4 flex items-start gap-2 rounded-lg bg-slate-950/60 p-3 text-xs leading-relaxed text-slate-400">
                  <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-emerald-400" />
                  <span>
                    Zugangsdaten werden nur für dieses Deployment verwendet und nicht gespeichert.
                    {delivery === "ssh"
                      ? " Ablauf: Plattform-Check → Release-Ordner → atomarer Wechsel → systemd-Dienst → Health-Check, bei Fehler automatischer Rollback."
                      : " Das CMS läuft im CGI-Modus (Apache mit .htaccess nötig). Eine bestehende .htaccess wird gesichert, eine vorhandene styrixa.db nie überschrieben."}
                  </span>
                </div>
                {delivery === "ftp" && (
                  <div className="mt-2 flex items-start gap-2 rounded-lg border border-amber-500/20 bg-amber-500/5 p-3 text-xs text-amber-200/90">
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                    Achtung: Der Document-Root wird vom STYRIXA CMS übernommen. Bestehende Website vorher sichern.
                  </div>
                )}
              </div>
            )}
          </div>
        </section>

        {/* ------------------------- Rechte Spalte ------------------------- */}
        <aside className="lg:sticky lg:top-6 lg:self-start">
          <div className="overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/70 backdrop-blur">
            <div className="border-b border-slate-800 p-5">
              <div className="mb-4 flex items-center justify-between">
                <h2 className="font-semibold text-white">Build-Übersicht</h2>
                <span className="rounded-md bg-emerald-500/10 px-2 py-0.5 font-mono text-[11px] text-emerald-300">
                  CGO_ENABLED=0
                </span>
              </div>

              <dl className="space-y-2 text-sm">
                <div className="flex justify-between">
                  <dt className="text-slate-500">Ziel</dt>
                  <dd className="font-mono text-slate-200">
                    {tgt.goos}/{tgt.goarch}
                  </dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-slate-500">Module</dt>
                  <dd className="text-slate-200">{activeModules.length}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-slate-500">Binary (geschätzt)</dt>
                  <dd className="font-mono text-slate-200">~{fmtSize(estKb)}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-slate-500">RAM im Betrieb</dt>
                  <dd className="font-mono text-slate-200">&lt; 20 MB</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-slate-500">Artefakt</dt>
                  <dd className="max-w-[200px] truncate font-mono text-xs text-slate-200" title={artifact}>
                    {artifact}
                  </dd>
                </div>
              </dl>

              <div className="mt-4 flex flex-wrap gap-1.5">
                {tags.map((t) => (
                  <span key={t} className="rounded bg-cyan-500/10 px-1.5 py-0.5 font-mono text-[10px] text-cyan-300">
                    {t}
                  </span>
                ))}
              </div>
            </div>

            {/* Befehl */}
            <div className="border-b border-slate-800 bg-slate-950/60 p-5">
              <div className="mb-2 flex items-center justify-between">
                <span className="flex items-center gap-1.5 text-xs text-slate-500">
                  <Terminal className="h-3.5 w-3.5" /> Build-Befehl (Worker)
                </span>
                <button onClick={copyCmd} className="flex items-center gap-1 text-xs text-slate-400 hover:text-white">
                  {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
                  {copied ? "Kopiert" : "Kopieren"}
                </button>
              </div>
              <pre className="overflow-x-auto whitespace-pre font-mono text-[11px] leading-relaxed text-emerald-300/90">
                {buildCmd}
              </pre>
            </div>

            {/* Build-Prozess */}
            <div className="p-5">
              {phase !== "idle" && (
                <>
                  <div className="mb-3 h-1.5 overflow-hidden rounded-full bg-slate-800">
                    <div
                      className="h-full rounded-full bg-gradient-to-r from-cyan-400 to-emerald-400 transition-all duration-500"
                      style={{ width: `${phase === "done" ? 100 : progress}%` }}
                    />
                  </div>
                  <ol className="mb-4 space-y-1.5">
                    {BUILD_STEPS.map((s, i) => {
                      const done = i < stepIdx || phase === "done";
                      const active = i === stepIdx && phase === "running";
                      return (
                        <li key={s.key} className="flex items-center gap-2 text-xs">
                          {done ? (
                            <Check className="h-3.5 w-3.5 text-emerald-400" />
                          ) : active ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin text-cyan-300" />
                          ) : (
                            <span className="h-3.5 w-3.5 rounded-full border border-slate-700" />
                          )}
                          <span className={done ? "text-slate-300" : active ? "text-white" : "text-slate-600"}>
                            {s.label}
                          </span>
                        </li>
                      );
                    })}
                  </ol>
                  <div
                    ref={logRef}
                    className="mb-4 h-44 overflow-y-auto rounded-lg border border-slate-800 bg-black/60 p-3 font-mono text-[10.5px] leading-relaxed"
                  >
                    {logs.map((l, i) => (
                      <div
                        key={i}
                        className={
                          l.includes("✓") ? "text-emerald-300" : l.startsWith("▸") ? "text-cyan-300" : "text-slate-400"
                        }
                      >
                        {l}
                      </div>
                    ))}
                    {phase === "running" && <span className="inline-block h-3 w-1.5 animate-pulse bg-cyan-300" />}
                  </div>
                </>
              )}

              {phase === "done" && result ? (
                <div className="space-y-3">
                  <div className="rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-4">
                    <div className="mb-1 flex items-center gap-2 text-sm font-semibold text-emerald-200">
                      <ShieldCheck className="h-4 w-4" /> Build erfolgreich
                    </div>
                    <div className="font-mono text-[11px] text-emerald-200/70">
                      {result.id} · {result.at} · Binary {result.bin} · Paket {result.size}
                    </div>
                  </div>
                  <button
                    onClick={() => alert(`Mockup: Download von ${result.file}`)}
                    className="flex w-full items-center justify-center gap-2 rounded-xl bg-white px-4 py-3 font-semibold text-slate-950 transition hover:bg-slate-200"
                  >
                    <Download className="h-4 w-4" /> {result.file}
                  </button>
                  <div className="rounded-lg bg-slate-950/60 p-3 font-mono text-[10.5px] leading-relaxed text-slate-400">
                    <div className="text-slate-500"># Auf dem Server</div>
                    tar -xzf {result.file}
                    <br />./{binName} --addr :8080
                    <br />
                    <span className="text-slate-500"># → http://host:8080/install</span>
                  </div>
                  {delivery !== "download" && (
                    <DeployPanel
                      delivery={delivery}
                      phase={deployPhase}
                      logs={deployLogs}
                      result={deployResult}
                      errors={deployErrors}
                      host={dep.host}
                      onDeploy={() => startDeploy()}
                    />
                  )}
                  <button
                    onClick={resetBuild}
                    className="flex w-full items-center justify-center gap-2 rounded-xl border border-slate-700 px-4 py-2.5 text-sm text-slate-300 hover:border-slate-500"
                  >
                    <RotateCcw className="h-4 w-4" /> Neue Konfiguration
                  </button>
                </div>
              ) : (
                <button
                  onClick={startBuild}
                  disabled={locked}
                  className="group flex w-full items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-cyan-400 to-emerald-400 px-4 py-3.5 font-bold text-slate-950 shadow-lg shadow-cyan-500/20 transition hover:brightness-110 disabled:cursor-wait disabled:opacity-80"
                >
                  {locked ? (
                    <>
                      <Loader2 className="h-5 w-5 animate-spin" /> Kompiliere …
                    </>
                  ) : (
                    <>
                      <Rocket className="h-5 w-5 transition group-hover:-translate-y-0.5" /> Deploy / Build
                    </>
                  )}
                </button>
              )}
            </div>
          </div>

          <p className="mt-4 px-1 text-[11px] leading-relaxed text-slate-600">
            Reproduzierbar: gleiche Config + gleicher Template-Commit ergeben dasselbe Binary (-trimpath, vendored deps).
          </p>
        </aside>
      </main>

      {deployPhase === "confirm" && identity && (
        <IdentityModal identity={identity} host={dep.host} onConfirm={confirmIdentity}
          onCancel={() => { setIdentity(null); setDeployPhase("idle"); pushLog("warn", "Deployment abgebrochen – Fingerprint nicht bestätigt"); }} />
      )}

      <footer className="relative border-t border-slate-800/80 py-6 text-center text-xs text-slate-600">
        STYRIXA Digital Technology Studio · Graz · Factory Platform Preview
      </footer>
    </div>
  );
}

/* ------------------------- Hilfskomponenten ------------------------- */
const inputCls =
  "w-full rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-white outline-none focus:border-cyan-400 disabled:opacity-60";

function Field({ label, children, className = "" }) {
  return (
    <label className={"block " + className}>
      <span className="mb-1.5 block text-xs text-slate-400">{label}</span>
      {children}
    </label>
  );
}

function DeployPanel({ delivery, phase, logs, result, errors, host, onDeploy }) {
  const ref = useRef(null);
  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [logs]);
  const color = { ok: "text-emerald-300", warn: "text-amber-300", error: "text-rose-300", info: "text-slate-400" };
  return (
    <div className="rounded-xl border border-slate-800 bg-slate-950/50 p-4">
      <div className="mb-3 flex items-center gap-2 text-sm font-semibold text-white">
        <UploadCloud className="h-4 w-4 text-cyan-300" />
        {delivery === "ssh" ? "Auf Server deployen" : "Auf Webspace deployen"}
      </div>

      {phase === "idle" || phase === "error" ? (
        <>
          {errors.length > 0 ? (
            <ul className="mb-3 space-y-1 text-xs text-amber-300/90">
              {errors.map((e) => (
                <li key={e}>• {e}</li>
              ))}
            </ul>
          ) : (
            <p className="mb-3 text-xs text-slate-500">
              Ziel: <span className="font-mono text-slate-300">{host}</span> · Zugangsdaten aus Schritt 4
            </p>
          )}
          <button
            onClick={onDeploy}
            disabled={errors.length > 0}
            className="flex w-full items-center justify-center gap-2 rounded-lg bg-cyan-400 px-4 py-2.5 text-sm font-bold text-slate-950 transition hover:bg-cyan-300 disabled:cursor-not-allowed disabled:opacity-40"
          >
            <UploadCloud className="h-4 w-4" /> {phase === "error" ? "Erneut versuchen" : "Jetzt deployen"}
          </button>
        </>
      ) : null}

      {logs.length > 0 && (
        <div ref={ref} className="mt-3 max-h-40 overflow-y-auto rounded-lg bg-black/60 p-2.5 font-mono text-[10.5px] leading-relaxed">
          {logs.map((l, i) => (
            <div key={i} className={color[l.level] || "text-slate-400"}>
              <span className="text-slate-600">{l.t} </span>
              {l.msg}
            </div>
          ))}
          {phase === "running" && <span className="inline-block h-3 w-1.5 animate-pulse bg-cyan-300" />}
        </div>
      )}

      {phase === "done" && result && (
        <div className="mt-3 space-y-2">
          <div className="flex items-center gap-2 rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-3 text-sm text-emerald-200">
            <Check className="h-4 w-4" /> Live – Release <span className="font-mono text-xs">{result.release}</span>
          </div>
          {result.url && (
            <a href={result.url.replace(/\/$/, "") + "/install"} target="_blank" rel="noreferrer"
              className="flex items-center justify-center gap-2 rounded-lg bg-white px-3 py-2 text-sm font-semibold text-slate-950 hover:bg-slate-200">
              <Globe className="h-4 w-4" /> Installer öffnen
            </a>
          )}
          {result.installCode && (
            <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-100">
              <p className="text-[11px] uppercase tracking-wide text-amber-300/80">Installations-Code für /install</p>
              <p className="mt-1 select-all font-mono text-base tracking-widest">{result.installCode}</p>
              <p className="mt-1 text-[11px] text-amber-200/70">Nur einmal gültig. Steht auch in der Datei install-code im Datenordner.</p>
            </div>
          )}
          {result.hints?.map((h) => (
            <p key={h} className="text-[11px] text-slate-500">{h}</p>
          ))}
          {result.caddy && (
            <pre className="overflow-x-auto rounded-lg bg-black/60 p-2.5 font-mono text-[10.5px] text-emerald-300/90">{result.caddy}</pre>
          )}
        </div>
      )}
    </div>
  );
}

function IdentityModal({ identity, host, onConfirm, onCancel }) {
  const ssh = identity.kind === "ssh-host-key";
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/80 p-4 backdrop-blur-sm">
      <div className="w-full max-w-md rounded-2xl border border-slate-700 bg-slate-900 p-6 shadow-2xl">
        <div className="mb-4 flex items-start justify-between">
          <div className={`flex h-10 w-10 items-center justify-center rounded-xl ${identity.mismatch ? "bg-rose-500/15 text-rose-300" : "bg-cyan-500/15 text-cyan-300"}`}>
            {identity.mismatch ? <AlertTriangle className="h-5 w-5" /> : <Fingerprint className="h-5 w-5" />}
          </div>
          <button onClick={onCancel} className="text-slate-500 hover:text-white"><X className="h-5 w-5" /></button>
        </div>
        <h3 className="text-lg font-semibold text-white">
          {identity.mismatch ? "Server-Identität hat sich geändert" : ssh ? "SSH-Host-Key bestätigen" : "FTPS-Zertifikat bestätigen"}
        </h3>
        <p className="mt-2 text-sm leading-relaxed text-slate-400">
          {identity.mismatch
            ? "Der Fingerprint weicht vom zuletzt bestätigten Wert ab. Das kann ein Serverwechsel sein – oder ein Angriff. Nur fortfahren, wenn der Hoster die Änderung bestätigt."
            : `Erste Verbindung zu ${host}. Vergleiche den Fingerprint mit der Angabe in deinem Hoster-Panel, bevor du Zugangsdaten überträgst.`}
        </p>
        <div className="mt-4 rounded-lg border border-slate-700 bg-black/50 p-3">
          <div className="mb-1 text-[10px] uppercase tracking-wider text-slate-500">{ssh ? "Host-Key" : "Zertifikat"} · SHA-256</div>
          <div className="break-all font-mono text-xs text-cyan-200">{identity.fingerprint}</div>
        </div>
        <div className="mt-5 flex gap-2">
          <button onClick={onCancel} className="flex-1 rounded-lg border border-slate-700 px-4 py-2.5 text-sm text-slate-300 hover:border-slate-500">
            Abbrechen
          </button>
          <button onClick={onConfirm}
            className={`flex-1 rounded-lg px-4 py-2.5 text-sm font-bold ${identity.mismatch ? "bg-rose-500 text-white hover:bg-rose-400" : "bg-cyan-400 text-slate-950 hover:bg-cyan-300"}`}>
            <span className="inline-flex items-center gap-1.5"><KeyRound className="h-4 w-4" /> Vertrauen &amp; deployen</span>
          </button>
        </div>
      </div>
    </div>
  );
}
