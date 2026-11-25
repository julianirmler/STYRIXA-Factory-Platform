// Dünne fetch-Hülle um die factory-api. Fehlertexte der API sind deutsch
// (text/plain) und werden direkt angezeigt.
import { queryFor } from "./selection.js";
import { readSSE } from "./sse.js";

export class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

async function check(res) {
  if (res.ok) return res;
  const text = (await res.text().catch(() => "")).trim();
  throw new ApiError(res.status, text || `Anfrage fehlgeschlagen (HTTP ${res.status})`);
}

/** @returns {Promise<{release: string, modules: object[], targets: string[]}>} */
export async function getCatalog(signal) {
  const res = await check(await fetch("/api/catalog", { signal }));
  return res.json();
}

/** @returns {Promise<{id, file, sha256, size, release, modules, goos, goarch, downloadUrl}>} */
export async function selectPackage(selected, target, signal) {
  const res = await check(await fetch(`/api/packages/select?${queryFor(selected, target)}`, { signal }));
  return res.json();
}

/**
 * POST /api/packages/{id}/deploy als Server-Sent Events.
 * handlers: { log, identity, result, error } – jeweils mit geparstem JSON.
 */
export async function streamDeploy(id, body, handlers, signal) {
  const res = await check(
    await fetch(`/api/packages/${encodeURIComponent(id)}/deploy`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal,
    })
  );
  await readSSE(res.body, ({ event, data }) => {
    let parsed;
    try {
      parsed = JSON.parse(data);
    } catch {
      return;
    }
    handlers[event]?.(parsed);
  });
}
