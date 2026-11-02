import type { NodeInfo } from "./types";

const OS_LABEL: Record<string, string> = {
  darwin: "macOS",
  windows: "Windows",
  linux: "Linux",
  freebsd: "FreeBSD",
  openbsd: "OpenBSD",
  netbsd: "NetBSD",
};

/** Human-readable OS/arch for a catalog node (e.g. "Windows/amd64"). */
export function platformLabel(n: Pick<NodeInfo, "os" | "arch"> | null | undefined): string {
  if (!n?.os) return "";
  const os = OS_LABEL[n.os] || n.os;
  return n.arch ? `${os}/${n.arch}` : os;
}
