import { b64Decode, b64Encode, utf8 } from "./bytes.ts";
import { generateIdentity, identityFromJSON, identityToJSON, type Identity } from "./identity.ts";

const DB_NAME = "agent-mesh-e2e";
const STORE = "vault";
const RECORD_KEY = "client";
const PBKDF2_ITERS = 310000;

export type VaultRecord = {
  v: 1;
  salt: string;
  iv: string;
  ct: string;
  idHint: string;
};

let unlocked: Identity | null = null;

export function vaultIdentity(): Identity | null {
  return unlocked;
}

export function lockVault(): void {
  unlocked = null;
}

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, 1);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE)) db.createObjectStore(STORE);
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error || new Error("idb open failed"));
  });
}

async function idbGet(): Promise<VaultRecord | null> {
  if (typeof indexedDB === "undefined") return memoryGet();
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, "readonly");
    const req = tx.objectStore(STORE).get(RECORD_KEY);
    req.onsuccess = () => resolve((req.result as VaultRecord) || null);
    req.onerror = () => reject(req.error || new Error("idb get failed"));
  });
}

async function idbPut(rec: VaultRecord): Promise<void> {
  if (typeof indexedDB === "undefined") {
    memoryPut(rec);
    return;
  }
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, "readwrite");
    tx.objectStore(STORE).put(rec, RECORD_KEY);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error || new Error("idb put failed"));
  });
}

async function idbClear(): Promise<void> {
  if (typeof indexedDB === "undefined") {
    memoryClear();
    return;
  }
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, "readwrite");
    tx.objectStore(STORE).delete(RECORD_KEY);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error || new Error("idb clear failed"));
  });
}

let memRec: VaultRecord | null = null;
function memoryGet(): VaultRecord | null {
  return memRec;
}
function memoryPut(rec: VaultRecord): void {
  memRec = rec;
}
function memoryClear(): void {
  memRec = null;
}

async function deriveWrapKey(passphrase: string, salt: Uint8Array): Promise<CryptoKey> {
  const base = await crypto.subtle.importKey("raw", utf8(passphrase), "PBKDF2", false, ["deriveKey"]);
  return crypto.subtle.deriveKey(
    { name: "PBKDF2", salt, iterations: PBKDF2_ITERS, hash: "SHA-256" },
    base,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"],
  );
}

async function sealIdentity(id: Identity, passphrase: string): Promise<VaultRecord> {
  const salt = crypto.getRandomValues(new Uint8Array(16));
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const key = await deriveWrapKey(passphrase, salt);
  const pt = utf8(identityToJSON(id));
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv }, key, pt));
  return { v: 1, salt: b64Encode(salt), iv: b64Encode(iv), ct: b64Encode(ct), idHint: id.id };
}

async function openIdentity(rec: VaultRecord, passphrase: string): Promise<Identity> {
  const key = await deriveWrapKey(passphrase, b64Decode(rec.salt));
  const pt = await crypto.subtle.decrypt({ name: "AES-GCM", iv: b64Decode(rec.iv) }, key, b64Decode(rec.ct));
  return identityFromJSON(new TextDecoder().decode(pt));
}

export async function vaultStatus(): Promise<{ present: boolean; idHint: string; unlocked: boolean }> {
  const rec = await idbGet();
  return { present: !!rec, idHint: rec?.idHint || "", unlocked: !!unlocked };
}

export async function createVault(passphrase: string): Promise<Identity> {
  if (!passphrase) throw new Error("passphrase required");
  const id = await generateIdentity();
  await idbPut(await sealIdentity(id, passphrase));
  unlocked = id;
  return id;
}

export async function unlockVault(passphrase: string): Promise<Identity> {
  const rec = await idbGet();
  if (!rec) throw new Error("vault empty");
  const id = await openIdentity(rec, passphrase);
  unlocked = id;
  return id;
}

export async function changeVaultPassphrase(oldPass: string, newPass: string): Promise<void> {
  if (!newPass) throw new Error("passphrase required");
  const rec = await idbGet();
  if (!rec) throw new Error("vault empty");
  const id = unlocked || (await openIdentity(rec, oldPass));
  // verify old
  await openIdentity(rec, oldPass);
  await idbPut(await sealIdentity(id, newPass));
  unlocked = id;
}

export async function importVaultKeyJSON(raw: string, passphrase: string): Promise<Identity> {
  if (!passphrase) throw new Error("passphrase required");
  const id = await identityFromJSON(raw);
  await idbPut(await sealIdentity(id, passphrase));
  unlocked = id;
  return id;
}

export function exportUnlockedKeyJSON(): string {
  if (!unlocked) throw new Error("vault locked");
  return identityToJSON(unlocked);
}

export async function clearVault(): Promise<void> {
  unlocked = null;
  await idbClear();
}

/** Test helper: seal/open without IndexedDB. */
export async function vaultRoundTripForTest(passphrase: string): Promise<Identity> {
  memoryClear();
  return createVault(passphrase);
}
