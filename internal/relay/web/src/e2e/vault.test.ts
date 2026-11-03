import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { clearVault, createVault, exportUnlockedKeyJSON, importVaultKeyJSON, lockVault, unlockVault, vaultStatus } from "./vault.ts";

describe("vault", () => {
  it("creates, locks, unlocks, and round-trips JSON", async () => {
    await clearVault();
    const id = await createVault("correct horse battery");
    assert.ok(id.id);
    const st = await vaultStatus();
    assert.equal(st.present, true);
    assert.equal(st.unlocked, true);
    const raw = exportUnlockedKeyJSON();
    assert.match(raw, /ed25519_seed/);
    lockVault();
    assert.equal((await vaultStatus()).unlocked, false);
    const again = await unlockVault("correct horse battery");
    assert.equal(again.id, id.id);
    await clearVault();
    const imported = await importVaultKeyJSON(raw, "other pass");
    assert.equal(imported.id, id.id);
  });

  it("rejects wrong passphrase", async () => {
    await clearVault();
    await createVault("secret-one");
    lockVault();
    await assert.rejects(() => unlockVault("nope"), /OperationError|decrypt|fail|Error/i);
  });
});
