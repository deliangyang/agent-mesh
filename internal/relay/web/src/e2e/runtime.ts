import { E2EClient } from "./client.ts";
import { ensureMeshMode, isE2EMode } from "./mode.ts";
import { lockVault, unlockVault, vaultIdentity, vaultStatus, type VaultRecord } from "./vault.ts";
import type { Identity } from "./identity.ts";

let client: E2EClient | null = null;

export async function bootE2E(): Promise<void> {
  await ensureMeshMode();
  if (!isE2EMode()) {
    client = null;
    return;
  }
  const id = vaultIdentity();
  if (id) client = new E2EClient(id, "");
}

export function e2eClient(): E2EClient {
  if (!client) {
    const id = vaultIdentity();
    if (!id) throw new Error("e2e vault locked");
    client = new E2EClient(id, "");
  }
  return client;
}

export function hasE2EClient(): boolean {
  return Boolean(client || vaultIdentity());
}

export async function unlockE2E(passphrase: string): Promise<Identity> {
  await ensureMeshMode();
  const id = await unlockVault(passphrase);
  client = new E2EClient(id, "");
  return id;
}

export async function afterVaultReady(id: Identity): Promise<void> {
  client = new E2EClient(id, "");
  if (!isE2EMode()) return;
  // Rebuild catalog sockets under the new identity; old peers/sessions are stale.
  const { stopE2ECatalogLoop, startE2ECatalogLoop } = await import("./sync.ts");
  stopE2ECatalogLoop();
  startE2ECatalogLoop();
}

export function lockE2E(): void {
  lockVault();
  client = null;
}

export { vaultStatus };
export type { VaultRecord };
