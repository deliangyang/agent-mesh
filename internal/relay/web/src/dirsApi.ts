import { fetchJSON } from "./http";

export type DirEntry = {
  name: string;
  path: string;
};

export type DirListing = {
  path: string;
  parent?: string;
  home?: string;
  dirs: DirEntry[];
};

/** listServerDirs browses one directory on the node, over E2E RPC or the local node API. */
export async function listServerDirs(node: string, path: string): Promise<DirListing> {
  const { ensureMeshMode, isE2EMode } = await import("./e2e/mode.ts");
  await ensureMeshMode();
  if (isE2EMode()) {
    const { e2eClient } = await import("./e2e/runtime.ts");
    const resp = await e2eClient().call(node, "dirs", { path });
    if (!resp.ok) throw new Error(resp.error || "dirs failed");
    return (resp.body || {}) as DirListing;
  }
  const q = new URLSearchParams();
  if (path) q.set("path", path);
  const resp = await fetchJSON("/v1/openai/dirs?" + q.toString(), { credentials: "same-origin" });
  if (!resp.ok) throw new Error((await resp.text()) || `dirs http ${resp.status}`);
  return (await resp.json()) as DirListing;
}
