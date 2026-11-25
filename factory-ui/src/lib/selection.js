// Reine Auswahl-Logik. Module kommen aus GET /api/catalog:
// [{ id, nr, name, required?, requires? }]

const byId = (modules) => new Map(modules.map((m) => [m.id, m]));

/** Nur bekannte Module, Pflichtmodule immer aktiv, fehlende Abhängigkeiten ergänzt. */
export function normalize(selected, modules) {
  const idx = byId(modules);
  const next = new Set([...selected].filter((id) => idx.has(id)));
  for (const m of modules) if (m.required) next.add(m.id);
  return withDeps(next, idx).set;
}

function withDeps(set, idx) {
  const added = [];
  const stack = [...set];
  while (stack.length) {
    for (const dep of idx.get(stack.pop())?.requires ?? []) {
      if (idx.has(dep) && !set.has(dep)) {
        set.add(dep);
        added.push(dep);
        stack.push(dep);
      }
    }
  }
  return { set, added };
}

function dependentsOf(id, set, modules) {
  const out = [];
  const stack = [id];
  while (stack.length) {
    const cur = stack.pop();
    for (const m of modules) {
      if (set.has(m.id) && !out.includes(m.id) && m.requires?.includes(cur)) {
        out.push(m.id);
        stack.push(m.id);
      }
    }
  }
  return out;
}

const names = (ids, idx) => ids.map((id) => idx.get(id).name).join(", ");

/**
 * Schaltet ein Modul um. Aktivieren ergänzt fehlende Abhängigkeiten,
 * Abwählen entfernt abhängige Module. Pflichtmodule und unbekannte IDs
 * ändern nichts.
 * @returns {{ selected: Set<string>, notice: string|null }}
 */
export function toggle(selected, id, modules) {
  const idx = byId(modules);
  const mod = idx.get(id);
  const next = normalize(selected, modules);
  if (!mod || mod.required) return { selected: next, notice: null };

  if (next.has(id)) {
    const deps = dependentsOf(id, next, modules);
    next.delete(id);
    deps.forEach((d) => next.delete(d));
    return {
      selected: next,
      notice: deps.length ? `${names(deps, idx)} wurde mit abgewählt (benötigt ${mod.name}).` : null,
    };
  }
  next.add(id);
  const { added } = withDeps(next, idx);
  return {
    selected: next,
    notice: added.length ? `${names(added, idx)} automatisch aktiviert (benötigt von ${mod.name}).` : null,
  };
}

/** Sortierte, stabile Query für GET /api/packages/select. */
export function queryFor(selected, target) {
  const mods = [...selected].sort().join(",");
  return `modules=${encodeURIComponent(mods)}&target=${encodeURIComponent(target)}`;
}
