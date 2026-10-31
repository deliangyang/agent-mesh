import { reactive } from "vue";
import type { MeshTarget } from "./types";

export const meshTarget = reactive<MeshTarget>({
  node: "",
  engine: "",
  session: "",
  model: "",
  cwd: "",
});

export function threadIdOf(node: string, engine: string, session: string): string {
  if (!session) return "";
  return [node, engine, session].join("|");
}

export function parseThreadId(id: string): { node: string; engine: string; session: string } {
  const parts = String(id || "").split("|");
  return {
    node: parts[0] || "",
    engine: parts[1] || "",
    session: parts.slice(2).join("|"),
  };
}
