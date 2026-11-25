// Nur Darstellung je Modul-ID. Welche Module es gibt, kommt aus /api/catalog.
import {
  Blocks,
  FileText,
  Image as ImageIcon,
  LayoutDashboard,
  Package,
  Palette,
  Search,
  ShoppingCart,
  Users,
} from "lucide-react";

const META = {
  dashboard: {
    icon: LayoutDashboard,
    desc: "Besucher-Statistiken, Systemstatus und Schnellzugriffe.",
    features: ["Besucher-Stats", "Status", "Quick Actions"],
  },
  content: {
    icon: FileText,
    desc: "Drag-and-Drop-Blöcke, HTML Studio (Raw HTML/CSS), Auto-Save.",
    features: ["Block-Editor", "HTML Studio", "Auto-Save"],
  },
  media: {
    icon: ImageIcon,
    desc: "WebP/AVIF-Auto-Optimierung, Ordnerstruktur, Alt-Texte.",
    features: ["WebP/AVIF", "Ordner", "Alt-Texte"],
  },
  seo: {
    icon: Search,
    desc: "Meta, Sitemap, robots.txt, 301-Redirects, Schema.org JSON-LD, Answer-First.",
    features: ["Sitemap", "301", "JSON-LD"],
  },
  users: {
    icon: Users,
    desc: "RBAC (Admin, Redakteur, Autor, Kunde), 2FA (TOTP), Activity-Log.",
    features: ["RBAC", "2FA", "Activity-Log"],
  },
  design: {
    icon: Palette,
    desc: "Globale Theme-Tokens, Farben, Fonts und Templates.",
    features: ["Tokens", "Fonts", "Templates"],
  },
  themes: {
    icon: Package,
    desc: "ZIP-Upload, theme.json-Validierung, 1-Klick-Aktivierung.",
    features: ["ZIP-Upload", "theme.json", "1-Klick"],
  },
  shop: {
    icon: ShoppingCart,
    desc: "Produkte & Varianten, Lager, Bestellungen, DACH-Steuern, Versand, Gutscheine, Rechnungen.",
    features: ["Produkte", "Bestellungen", "USt AT/DE/CH", "Rechnungen"],
  },
};

const FALLBACK = { icon: Blocks, desc: "Zusatzmodul des STYRIXA CMS.", features: [] };

/** Darstellung für eine Modul-ID; unbekannte IDs bekommen einen neutralen Eintrag. */
export function moduleMeta(id) {
  return Object.hasOwn(META, id) ? META[id] : FALLBACK;
}

const TARGET_META = {
  "linux/amd64": { label: "Linux", sub: "x86_64 · Server/VPS" },
  "linux/arm64": { label: "Linux ARM", sub: "arm64 · Graviton/Pi" },
  "darwin/arm64": { label: "macOS", sub: "Apple Silicon" },
  "darwin/amd64": { label: "macOS Intel", sub: "x86_64" },
  "windows/amd64": { label: "Windows", sub: "x86_64 · .exe" },
};

/** Darstellung für eine Plattform „goos/goarch“ mit Fallback. */
export function targetMeta(target) {
  if (Object.hasOwn(TARGET_META, target)) return TARGET_META[target];
  const [goos = target, goarch = ""] = target.split("/");
  return { label: goos, sub: goarch };
}

/** Bytes → „12,3 MB“ (de-AT). */
export function formatSize(bytes) {
  const units = ["B", "KB", "MB", "GB"];
  let v = bytes;
  let u = 0;
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024;
    u++;
  }
  return `${v.toLocaleString("de-AT", { maximumFractionDigits: u ? 1 : 0 })} ${units[u]}`;
}
