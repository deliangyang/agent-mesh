const KEY = "mesh.theme";

export type MeshTheme = "light" | "dark";

export function loadTheme(): MeshTheme {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw === "dark" || raw === "light") return raw;
  } catch {
    /* ignore */
  }
  return "light";
}

export function applyTheme(theme: MeshTheme): void {
  const root = document.documentElement;
  root.classList.toggle("dark", theme === "dark");
  root.style.colorScheme = theme;
  try {
    localStorage.setItem(KEY, theme);
  } catch {
    /* ignore */
  }
}

export function toggleTheme(): MeshTheme {
  const next: MeshTheme = loadTheme() === "dark" ? "light" : "dark";
  applyTheme(next);
  return next;
}
