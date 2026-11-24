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

  function resetBuild() {
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

      <footer className="relative border-t border-slate-800/80 py-6 text-center text-xs text-slate-600">
        STYRIXA Digital Technology Studio · Graz · Factory Platform Preview
      </footer>
    </div>
  );
}
