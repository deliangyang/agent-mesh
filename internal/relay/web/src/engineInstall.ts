import { t } from "./locale.ts";
import type { EngineRow } from "./types";

type EngineMeta = { bin: string; env: string };

const META: Record<string, EngineMeta> = {
  workbuddy: { bin: "WorkBuddy", env: "WORKBUDDY_HOME" },
  claude: { bin: "claude", env: "CLAUDE_BIN" },
  cursor: { bin: "agent / cursor-agent", env: "CURSOR_AGENT_BIN" },
  codex: { bin: "codex", env: "CODEX_BIN" },
  opencode: { bin: "opencode", env: "OPENCODE_BIN" },
  pi: { bin: "pi", env: "PI_BIN" },
  "qoder-cn": { bin: "qodercn", env: "QODERCN_BIN" },
  qodercn: { bin: "qodercn", env: "QODERCN_BIN" },
};

/** Normalize GOOS / NodeInfo.os to a locale bucket. */
export function osBucket(os?: string): "windows" | "darwin" | "linux" | "default" {
  const v = String(os || "").toLowerCase();
  if (v === "windows" || v === "win32") return "windows";
  if (v === "darwin" || v === "macos" || v === "osx") return "darwin";
  if (v === "linux") return "linux";
  return "default";
}

export function engineMeta(name: string): EngineMeta {
  return META[name] || { bin: name, env: "" };
}

/** One-line install guidance for a missing engine on a node OS. */
export function formatEngineInstall(e: Pick<EngineRow, "name" | "title">, os?: string): string {
  const meta = engineMeta(e.name);
  const title = e.title || e.name;
  const where = t(`engine.installWhere.${osBucket(os)}`);
  if (meta.env) {
    return t("engine.installLine", { title, bin: meta.bin, env: meta.env }) + " " + where;
  }
  return t("engine.installLineNoEnv", { title, bin: meta.bin }) + " " + where;
}
