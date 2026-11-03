/** Client-side mesh.json-shaped export/import for the Settings relay panel. */

export type MeshConfigBundle = {
  version: 1;
  relay: string;
  password: string;
  advertise: string;
  /** Passphrase for browser E2E vault (import only; never sent to relay). */
  vault_passphrase?: string;
  /** Raw client.key JSON or stringified identity file. */
  client_key?: string;
};

export function buildMeshConfigBundle(input: {
  relay: string;
  password: string;
  advertise: string;
}): MeshConfigBundle {
  return {
    version: 1,
    relay: String(input.relay || "").trim(),
    password: String(input.password || ""),
    advertise: String(input.advertise || "").trim(),
  };
}

/** Parse a Mesh config download; returns null when the payload is unusable. */
export function parseMeshConfigBundle(raw: string): MeshConfigBundle | null {
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!data || typeof data !== "object") return null;
  const obj = data as Record<string, unknown>;
  const relay = typeof obj.relay === "string" ? obj.relay : typeof obj.relays === "string" ? obj.relays : "";
  const relays = Array.isArray(obj.relays) ? obj.relays.filter((x): x is string => typeof x === "string") : [];
  const password = typeof obj.password === "string" ? obj.password : "";
  const advertise = typeof obj.advertise === "string" ? obj.advertise : "";
  const vault_passphrase = typeof obj.vault_passphrase === "string" ? obj.vault_passphrase : "";
  let client_key = "";
  if (typeof obj.client_key === "string") client_key = obj.client_key;
  else if (obj.client_key && typeof obj.client_key === "object") {
    try {
      client_key = JSON.stringify(obj.client_key);
    } catch {
      client_key = "";
    }
  }
  const joined = relay.trim() || relays.map((s) => s.trim()).filter(Boolean).join(", ");
  if (!joined && !password && !advertise && !vault_passphrase && !client_key) return null;
  const bundle = buildMeshConfigBundle({ relay: joined, password, advertise });
  if (vault_passphrase) bundle.vault_passphrase = vault_passphrase;
  if (client_key) bundle.client_key = client_key;
  return bundle;
}

export function meshConfigFilename(when = new Date()): string {
  const y = when.getFullYear();
  const m = String(when.getMonth() + 1).padStart(2, "0");
  const d = String(when.getDate()).padStart(2, "0");
  return `mesh-config-${y}${m}${d}.json`;
}
