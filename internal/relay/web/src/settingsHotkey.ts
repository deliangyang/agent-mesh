/** True when the event is the Mesh "open settings" chord (Ctrl/Cmd + ,). */
export function isOpenSettingsHotkey(ev: { key: string; metaKey: boolean; ctrlKey: boolean; altKey: boolean; shiftKey: boolean }): boolean {
  if (ev.key !== ",") return false;
  if (!(ev.metaKey || ev.ctrlKey)) return false;
  if (ev.altKey || ev.shiftKey) return false;
  return true;
}
