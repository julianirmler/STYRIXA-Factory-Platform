import React, { useEffect, useMemo, useReducer, useRef, useState } from "react";
import {
  Lock,
  Check,
  Cpu,
  Server,
  Monitor,
  Laptop,
  Rocket,
  Download,
  Loader2,
  Database,
  ShieldCheck,
  Info,
  Hash,
  Boxes,
  UploadCloud,
  KeyRound,
  Globe,
  AlertTriangle,
  Fingerprint,
  Copy,
  RotateCcw,
  X,
} from "lucide-react";
import { getCatalog, selectPackage, streamDeploy as apiStreamDeploy } from "./lib/api.js";
import { normalize, toggle as toggleSelection } from "./lib/selection.js";
import { formatSize, moduleMeta, targetMeta } from "./lib/moduleMeta.js";
import { deployBody, deployReducer, hostKeyCheckCommand, initialDeploy, pinKey, validateDeploy } from "./lib/deployFlow.js";

/* ------------------------------------------------------------------ */
/*  STYRIXA Factory – Configurator                                     */
/*  Module, Abhängigkeiten und Plattformen kommen aus GET /api/catalog */
/*  (signierte modules.json). Die Factory baut nicht selbst (ADR-004): */
/*  jede Auswahl wird einem vorgebauten, signierten Paket zugeordnet.  */
/* ------------------------------------------------------------------ */

const TARGET_ICONS = { "linux/amd64": Server, "linux/arm64": Cpu, darwin: Laptop, windows: Monitor };
const targetIcon = (t) => TARGET_ICONS[t] || TARGET_ICONS[t.split("/")[0]] || Server;

// Auslieferungswege
const DELIVERY = [
  { id: "download", label: "Download", sub: ".tar.gz selbst installieren", icon: Download },
  { id: "ssh", label: "Server per SSH", sub: "VPS, Managed Server, Hosting mit SSH", icon: Server, badge: "Empfohlen" },
  { id: "ftp", label: "Webspace per FTP(S)", sub: "Shared Hosting mit Apache (CGI-Modus)", icon: Globe },
];

const SELECT_DEBOUNCE_MS = 150;

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
  // Katalog
  const [catalog, setCatalog] = useState(null); // {release, modules, targets}
  const [catalogError, setCatalogError] = useState(null);
  const [catalogTry, setCatalogTry] = useState(0);

  const [selected, setSelected] = useState(() => new Set(["seo", "media"]));
  const [target, setTarget] = useState("linux/amd64");
  const [notice, setNotice] = useState(null);

  // Paket zur Auswahl
  const [pkg, setPkg] = useState(null);
  const [pkgLoading, setPkgLoading] = useState(false);
  const [pkgError, setPkgError] = useState(null);

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
  const [deploy, dispatch] = useReducer(deployReducer, initialDeploy);
  // Nur im Speicher dieser Seite, nie in localStorage:
  const pins = useRef(new Map()); // bestätigte Fingerprints je Ziel (pinKey)
  const pending = useRef(null); // Anfrage inkl. Zugangsdaten bis zum Ende des Ablaufs
  const deployAbort = useRef(null);

  /* ---- Katalog laden ---- */
  useEffect(() => {
    const ac = new AbortController();
    setCatalogError(null);
    getCatalog(ac.signal)
      .then((c) => {
        setCatalog(c);
        setSelected((s) => normalize(s, c.modules));
        setTarget((t) => (c.targets.includes(t) ? t : c.targets[0]));
      })
      .catch((e) => {
        if (e.name === "AbortError") return;
        setCatalogError(e.status === 503 ? "Katalog derzeit nicht verfügbar" : e.message);
      });
    return () => ac.abort();
  }, [catalogTry]);

  /* ---- Paket zur Auswahl (entprellt, vorherige Anfrage abbrechen) ---- */
  useEffect(() => {
    if (!catalog || !target) return;
    const ac = new AbortController();
    setPkgLoading(true);
    setPkgError(null);
    const t = setTimeout(() => {
      selectPackage(selected, target, ac.signal)
        .then((p) => {
          setPkg(p);
          setPkgLoading(false);
        })
        .catch((e) => {
          if (e.name === "AbortError") return;
          setPkg(null);
          setPkgError(e.message);
          setPkgLoading(false);
        });
    }, SELECT_DEBOUNCE_MS);
    return () => {
      clearTimeout(t);
      ac.abort();
    };
  }, [catalog, selected, target]);

  useEffect(() => () => deployAbort.current?.abort(), []);

  // Neues Paket → laufendes Deploy-Ergebnis gehört nicht mehr dazu
  useEffect(() => {
    resetDeploy();
  }, [pkg?.id]);

  const modules = catalog?.modules ?? [];
  const targets = catalog?.targets ?? [];
  const activeModules = modules.filter((m) => selected.has(m.id));
  const [goos, goarch] = target.split("/");
  const binName = `styrixa${goos === "windows" ? ".exe" : ""}`;
  const depLocked = deploy.phase === "running" || deploy.phase === "confirm";

  function toggle(mod) {
    if (mod.required || depLocked) return;
    const r = toggleSelection(selected, mod.id, modules);
    setSelected(r.selected);
    setNotice(r.notice);
  }

  function selectAll() {
    if (depLocked) return;
    setSelected(normalize(new Set(modules.map((m) => m.id)), modules));
    setNotice(null);
  }

  function selectMinimal() {
    if (depLocked) return;
    setSelected(normalize(new Set(), modules));
    setNotice(null);
  }

  function dropCredentials() {
    pending.current = null;
  }

  function resetDeploy() {
    deployAbort.current?.abort();
    deployAbort.current = null;
    dropCredentials();
    dispatch({ type: "reset" });
  }

  const setD = (k) => (e) => setDep((d) => ({ ...d, [k]: e.target.value }));

  const deployErrors = useMemo(() => validateDeploy(delivery, dep, target), [delivery, dep, target]);

  // POST /api/packages/{id}/deploy → Server-Sent Events
  function runDeploy(body) {
    deployAbort.current?.abort();
    const ac = new AbortController();
    deployAbort.current = ac;
    const live = (fn) => (d) => {
      if (!ac.signal.aborted) fn(d);
    };
    apiStreamDeploy(
      pkg.id,
      body,
      {
        log: live((d) => dispatch({ type: "log", level: d.level, msg: d.msg })),
        identity: live((d) => dispatch({ type: "identity", identity: d })),
        result: live((d) => {
          dropCredentials();
          dispatch({ type: "result", result: d });
        }),
        error: live((d) => {
          dropCredentials();
          dispatch({ type: "error", message: d.message });
        }),
      },
      ac.signal
    )
      .then(() => {
        if (!ac.signal.aborted) dispatch({ type: "end" });
      })
      .catch((e) => {
        if (ac.signal.aborted || e.name === "AbortError") return;
        dropCredentials();
        dispatch({ type: "error", message: e.message });
      });
  }

  function startDeploy() {
    if (!pkg || deployErrors.length) return;
    const pin = pins.current.get(pinKey(delivery, dep.host, dep.port));
    const body = deployBody(delivery, dep, pin);
    pending.current = body;
    // Zugangsdaten sofort aus dem Formular entfernen
    setDep((d) => ({ ...d, password: "", privateKey: "" }));
    dispatch({ type: "start" });
    runDeploy(body);
  }

  function confirmIdentity(acknowledged) {
    const id = deploy.identity;
    const body = pending.current;
    if (!id || !body || (id.mismatch && !acknowledged)) return;
    const t = body.target;
    pins.current.set(pinKey(t.method, t.host, t.port), id.fingerprint);
    const next = { ...body, target: { ...t, fingerprint: id.fingerprint } };
    pending.current = next;
    dispatch({ type: "confirm", acknowledged });
    runDeploy(next);
  }

  function cancelIdentity() {
    dropCredentials();
    dispatch({ type: "cancel" });
  }

  /* ---- Lade- und Fehlerzustand des Katalogs ---- */
  if (!catalog) {
    return (
      <Shell release={null}>
        <div className="mx-auto flex max-w-md flex-col items-center gap-4 px-6 py-32 text-center">
          {catalogError ? (
            <>
              <AlertTriangle className="h-8 w-8 text-amber-300" />
              <p className="text-lg font-semibold text-white">{catalogError}</p>
              <p className="text-sm text-slate-400">Die Paketliste konnte nicht geladen oder geprüft werden.</p>
              <button
                onClick={() => setCatalogTry((n) => n + 1)}
                className="flex items-center gap-2 rounded-lg border border-slate-700 px-4 py-2 text-sm text-slate-200 hover:border-slate-500"
              >
                <RotateCcw className="h-4 w-4" /> Erneut versuchen
              </button>
            </>
          ) : (
            <>
              <Loader2 className="h-8 w-8 animate-spin text-cyan-300" />
              <p className="text-sm text-slate-400">Lade signierten Paket-Katalog …</p>
            </>
          )}
        </div>
      </Shell>
    );
  }

  return (
    <Shell release={catalog.release}>
      <main className="relative mx-auto grid max-w-7xl gap-8 px-6 py-10 lg:grid-cols-[1fr_400px]">
        {/* ------------------------- Linke Spalte ------------------------- */}
        <section>
          <div className="mb-8">
            <p className="mb-2 font-mono text-xs uppercase tracking-[0.25em] text-cyan-400">Schritt 1 · Module</p>
            <h1 className="text-3xl font-bold tracking-tight text-white md:text-4xl">
              Dein STYRIXA CMS, als ein einziges Binary.
            </h1>
            <p className="mt-3 max-w-2xl text-slate-400">
              Wähle die Module. Jede Kombination liegt als CGO-freies Go-Binary mit eingebetteter SQLite und
              Web-Installer vorgebaut und signiert bereit. Nicht gewählte Module sind gar nicht erst im Binary.
            </p>
          </div>

          <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
            <div className="text-sm text-slate-400">
              <span className="font-semibold text-white">{activeModules.length}</span> / {modules.length} Module aktiv
            </div>
            <div className="flex gap-2">
              <button
                onClick={selectMinimal}
                disabled={depLocked}
                className="rounded-lg border border-slate-700 px-3 py-1.5 text-xs text-slate-300 transition hover:border-slate-500 disabled:opacity-40"
              >
                Minimal
              </button>
              <button
                onClick={selectAll}
                disabled={depLocked}
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
            {modules.map((m) => {
              const on = selected.has(m.id);
              const meta = moduleMeta(m.id);
              const Icon = meta.icon;
              return (
                <button
                  key={m.id}
                  onClick={() => toggle(m)}
                  disabled={depLocked}
                  aria-pressed={on}
                  className={[
                    "group relative overflow-hidden rounded-2xl border p-5 text-left transition",
                    on
                      ? "border-cyan-400/50 bg-gradient-to-br from-cyan-500/10 to-emerald-500/5 shadow-[0_0_0_1px_rgba(34,211,238,.15),0_10px_40px_-15px_rgba(34,211,238,.4)]"
                      : "border-slate-800 bg-slate-900/50 hover:border-slate-600",
                    m.required ? "cursor-default" : "cursor-pointer",
                    depLocked ? "opacity-70" : "",
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

                  <p className="mb-4 text-sm leading-relaxed text-slate-400">{meta.desc}</p>

                  {meta.features.length > 0 && (
                    <div className="flex flex-wrap items-center gap-1.5">
                      {meta.features.map((f) => (
                        <span key={f} className="rounded-md bg-slate-800/80 px-2 py-0.5 text-[11px] text-slate-300">
                          {f}
                        </span>
                      ))}
                    </div>
                  )}

                  <div className="mt-4 flex items-center justify-between border-t border-slate-800 pt-3 font-mono text-[11px]">
                    <span className={on ? "text-cyan-300" : "text-slate-500"}>mod_{m.id}</span>
                    <span className="text-slate-500">
                      {m.required && "Pflicht"}
                      {m.requires?.length > 0 && (
                        <span className="ml-2 text-amber-300/80">↳ mod_{m.requires.join(", mod_")}</span>
                      )}
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
              {targets.map((t) => {
                const on = t === target;
                const Icon = targetIcon(t);
                const meta = targetMeta(t);
                return (
                  <button
                    key={t}
                    disabled={depLocked}
                    onClick={() => setTarget(t)}
                    className={[
                      "rounded-xl border p-4 text-left transition disabled:opacity-60",
                      on ? "border-cyan-400/60 bg-cyan-500/10" : "border-slate-800 bg-slate-900/50 hover:border-slate-600",
                    ].join(" ")}
                  >
                    <Icon className={`mb-3 h-5 w-5 ${on ? "text-cyan-300" : "text-slate-500"}`} />
                    <div className="text-sm font-semibold text-white">{meta.label}</div>
                    <div className="text-xs text-slate-500">{meta.sub}</div>
                    <div className="mt-2 font-mono text-[10px] text-slate-600">{t}</div>
                  </button>
                );
              })}
            </div>
          </div>

          {/* Auslieferung */}
          <div className="mt-12">
            <p className="mb-2 font-mono text-xs uppercase tracking-[0.25em] text-cyan-400">Schritt 3 · Auslieferung</p>
            <div className="grid gap-3 md:grid-cols-3">
              {DELIVERY.map((d) => {
                const on = d.id === delivery;
                const Icon = d.icon;
                return (
                  <button
                    key={d.id}
                    disabled={depLocked}
                    onClick={() => {
                      setDelivery(d.id);
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
                <h2 className="font-semibold text-white">Paket</h2>
                {pkgLoading ? (
                  <Loader2 className="h-4 w-4 animate-spin text-cyan-300" />
                ) : (
                  <span className="rounded-md bg-emerald-500/10 px-2 py-0.5 font-mono text-[11px] text-emerald-300">
                    CGO_ENABLED=0
                  </span>
                )}
              </div>

              <dl className="space-y-2 text-sm">
                <Row label="Ziel" value={target} mono />
                <Row label="Module" value={activeModules.length} />
                <Row label="Paket-ID" value={pkg?.id ?? "—"} mono small />
                <Row label="Datei" value={pkg?.file ?? "—"} mono small />
                <Row label="Größe" value={pkg ? formatSize(pkg.size) : "—"} mono />
                <Row label="SHA-256" value={pkg ? pkg.sha256.slice(0, 12) + "…" : "—"} title={pkg?.sha256} mono small />
                <Row label="Release" value={pkg?.release ?? catalog.release} mono />
                <Row label="RAM im Betrieb" value="< 20 MB" mono />
              </dl>

              <div className="mt-4 flex flex-wrap gap-1.5">
                {activeModules.map((m) => (
                  <span key={m.id} className="rounded bg-cyan-500/10 px-1.5 py-0.5 font-mono text-[10px] text-cyan-300">
                    mod_{m.id}
                  </span>
                ))}
              </div>
            </div>

            <div className="space-y-3 p-5">
              {pkgError && (
                <div className="flex items-start gap-2 rounded-lg border border-rose-500/30 bg-rose-500/10 p-3 text-sm text-rose-200">
                  <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                  {pkgError}
                </div>
              )}

              {pkg && !pkgLoading ? (
                <a
                  href={pkg.downloadUrl}
                  download={pkg.file}
                  className="group flex w-full items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-cyan-400 to-emerald-400 px-4 py-3.5 font-bold text-slate-950 shadow-lg shadow-cyan-500/20 transition hover:brightness-110"
                >
                  <Download className="h-5 w-5 transition group-hover:translate-y-0.5" /> Herunterladen
                </a>
              ) : (
                <span
                  aria-disabled="true"
                  className="flex w-full cursor-not-allowed items-center justify-center gap-2 rounded-xl bg-slate-800 px-4 py-3.5 font-bold text-slate-500"
                >
                  {pkgLoading ? <Loader2 className="h-5 w-5 animate-spin" /> : <Download className="h-5 w-5" />} Herunterladen
                </span>
              )}
              <p className="flex items-center justify-center gap-1.5 text-xs text-emerald-300/80">
                <ShieldCheck className="h-3.5 w-3.5" /> Signiert und geprüft
              </p>

              {pkg && (
                <div className="rounded-lg bg-slate-950/60 p-3 font-mono text-[10.5px] leading-relaxed text-slate-400">
                  <div className="text-slate-500"># Auf dem Server</div>
                  tar -xzf {pkg.file}
                  <br />./{binName} --addr :8080
                  <br />
                  <span className="text-slate-500"># → http://host:8080/install</span>
                </div>
              )}

              {delivery !== "download" && pkg && (
                <DeployPanel
                  delivery={delivery}
                  phase={deploy.phase}
                  logs={deploy.logs}
                  result={deploy.result}
                  errors={deployErrors}
                  host={dep.host}
                  pkgId={pkg.id}
                  tunnel={delivery === "ssh" && dep.host ? `ssh -L 8080:127.0.0.1:${Number(dep.appPort) || 8080} ${dep.user}@${dep.host}${dep.port ? " -p " + dep.port : ""}` : ""}
                  onDeploy={startDeploy}
                />
              )}
            </div>
          </div>

          <p className="mt-4 px-1 text-[11px] leading-relaxed text-slate-600">
            Alle Kombinationen werden bei jedem Release in der CI gebaut und per ed25519 signiert. Die Factory prüft
            die Prüfsumme bei jedem Abruf.
          </p>
        </aside>
      </main>

      {deploy.phase === "confirm" && deploy.identity && (
        <IdentityModal identity={deploy.identity} host={dep.host} onConfirm={confirmIdentity} onCancel={cancelIdentity} />
      )}
    </Shell>
  );
}

/* ------------------------- Rahmen ------------------------- */
function Shell({ release, children }) {
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
            {release && (
              <span className="hidden items-center gap-1.5 md:flex">
                <span className="h-2 w-2 rounded-full bg-emerald-400 shadow-[0_0_8px] shadow-emerald-400" />
                Katalog geprüft
              </span>
            )}
            <span className="font-mono">cms {release ?? "…"}</span>
          </div>
        </div>
      </header>

      {children}

      <footer className="relative border-t border-slate-800/80 py-6 text-center text-xs text-slate-600">
        STYRIXA Digital Technology Studio · Graz · Factory Platform Preview
      </footer>
    </div>
  );
}

function Row({ label, value, title, mono, small }) {
  return (
    <div className="flex justify-between gap-4">
      <dt className="shrink-0 text-slate-500">{label}</dt>
      <dd
        className={["truncate text-slate-200", mono ? "font-mono" : "", small ? "text-xs leading-5" : ""].join(" ")}
        title={title ?? String(value)}
      >
        {value}
      </dd>
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

function DeployPanel({ delivery, phase, logs, result, errors, host, tunnel, pkgId, onDeploy }) {
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
              Ziel: <span className="font-mono text-slate-300">{host}</span> · Zugangsdaten aus Schritt 3
            </p>
          )}
          <p className="mb-3 text-xs text-slate-500">
            Paket: <span className="font-mono text-slate-300">{pkgId}</span>
          </p>
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
          {result.installCode && <InstallCode code={result.installCode} />}
          {result.url ? (
            <a href={result.url.replace(/\/$/, "") + "/install"} target="_blank" rel="noreferrer"
              className="flex items-center justify-center gap-2 rounded-lg bg-white px-3 py-2 text-sm font-semibold text-slate-950 hover:bg-slate-200">
              <Globe className="h-4 w-4" /> Installer öffnen
            </a>
          ) : (
            tunnel && (
              <div className="rounded-lg bg-black/60 p-2.5 font-mono text-[10.5px] leading-relaxed text-slate-300">
                <div className="text-slate-500"># Ohne Domain: per SSH-Tunnel öffnen</div>
                {tunnel}
                <div className="text-slate-500"># → http://127.0.0.1:8080/install</div>
              </div>
            )
          )}
          {result.hints?.map((h) => (
            <p key={h} className="text-[11px] text-slate-500">{h}</p>
          ))}
          {Object.entries(result.snippets ?? {}).map(([name, text]) => (
            <details key={name} className="rounded-lg bg-black/60 p-2.5 text-[11px] text-slate-400">
              <summary className="cursor-pointer select-none">{name === "caddy" ? "Caddy" : name === "nginx" ? "nginx" : name}-Konfiguration</summary>
              <pre className="mt-2 overflow-x-auto font-mono text-[10.5px] text-emerald-300/90">{text}</pre>
            </details>
          ))}
        </div>
      )}
    </div>
  );
}

function InstallCode({ code }) {
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* Zwischenablage nicht verfügbar – Code ist markierbar */
    }
  }
  return (
    <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 p-4 text-amber-100">
      <p className="text-[11px] uppercase tracking-wide text-amber-300/80">Installations-Code für /install</p>
      <div className="mt-2 flex items-center justify-between gap-3">
        <p className="select-all break-all font-mono text-2xl font-bold tracking-widest">{code}</p>
        <button onClick={copy} title="Kopieren"
          className="flex shrink-0 items-center gap-1 rounded-md border border-amber-400/40 px-2 py-1.5 text-xs hover:bg-amber-400/10">
          {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />} {copied ? "Kopiert" : "Kopieren"}
        </button>
      </div>
      <p className="mt-2 text-xs font-semibold text-amber-200">Nur einmal angezeigt – jetzt notieren.</p>
    </div>
  );
}

function IdentityModal({ identity, host, onConfirm, onCancel }) {
  const ssh = identity.kind === "ssh-host-key";
  const [ack, setAck] = useState(false);
  const blocked = identity.mismatch && !ack;
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
          <div className="mb-1 text-[10px] uppercase tracking-wider text-slate-500">
            {ssh ? "Host-Key" : "Zertifikat"}{identity.keyType ? ` · ${identity.keyType}` : ""} · SHA-256
          </div>
          <div className="break-all font-mono text-xs text-cyan-200">{identity.fingerprint}</div>
        </div>
        {ssh && hostKeyCheckCommand(identity.keyType) && (
          <p className="mt-2 text-xs leading-relaxed text-slate-500">
            In der Server-Konsole denselben Schlüsseltyp prüfen:{" "}
            <code className="break-all font-mono text-slate-300">{hostKeyCheckCommand(identity.keyType)}</code>
          </p>
        )}
        {identity.mismatch && (
          <label className="mt-4 flex items-start gap-2 rounded-lg border border-rose-500/40 bg-rose-500/10 p-3 text-xs text-rose-200">
            <input type="checkbox" checked={ack} onChange={(e) => setAck(e.target.checked)} className="mt-0.5" />
            Ich habe den neuen Fingerprint mit dem Hoster abgeglichen und will trotzdem fortfahren.
          </label>
        )}
        <div className="mt-5 flex gap-2">
          <button onClick={onCancel} className="flex-1 rounded-lg border border-slate-700 px-4 py-2.5 text-sm text-slate-300 hover:border-slate-500">
            Abbrechen
          </button>
          <button onClick={() => onConfirm(identity.mismatch ? ack : false)} disabled={blocked}
            className={`flex-1 rounded-lg px-4 py-2.5 text-sm font-bold disabled:cursor-not-allowed disabled:opacity-40 ${identity.mismatch ? "bg-rose-500 text-white hover:bg-rose-400" : "bg-cyan-400 text-slate-950 hover:bg-cyan-300"}`}>
            <span className="inline-flex items-center gap-1.5"><KeyRound className="h-4 w-4" /> {identity.mismatch ? "Trotzdem vertrauen" : "Vertrauen & deployen"}</span>
          </button>
        </div>
      </div>
    </div>
  );
}
