/** Best-effort label for the browser host OS (Settings connection panel). */

export function clientOsKey(ua = "", platform = ""): string {
  const hay = `${ua} ${platform}`.toLowerCase();
  if (/windows|win32|win64/.test(hay)) return "windows";
  if (/android/.test(hay)) return "android";
  if (/iphone|ipad|ipod|ios/.test(hay)) return "ios";
  if (/mac os|macintosh|macintel/.test(hay)) return "darwin";
  if (/cros/.test(hay)) return "chromeos";
  if (/linux|x11/.test(hay)) return "linux";
  return "unknown";
}

const LABEL: Record<string, string> = {
  windows: "Windows",
  android: "Android",
  ios: "iOS",
  darwin: "macOS",
  chromeos: "Chrome OS",
  linux: "Linux",
  unknown: "Unknown",
};

export function clientPlatformLabel(ua?: string, platform?: string): string {
  const u = ua ?? (typeof navigator !== "undefined" ? navigator.userAgent : "");
  const p = platform ?? (typeof navigator !== "undefined" ? navigator.platform : "");
  return LABEL[clientOsKey(u, p)] || LABEL.unknown;
}
