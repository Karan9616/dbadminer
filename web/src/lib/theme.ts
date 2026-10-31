export type ThemePreference = "light" | "dark" | "system";

const KEY = "dbadminer:theme";
const media = window.matchMedia("(prefers-color-scheme: dark)");

export function loadThemePreference(): ThemePreference {
  const v = localStorage.getItem(KEY);
  return v === "light" || v === "dark" ? v : "system";
}

export function saveThemePreference(pref: ThemePreference): void {
  if (pref === "system") localStorage.removeItem(KEY);
  else localStorage.setItem(KEY, pref);
}

export function resolveTheme(pref: ThemePreference): "light" | "dark" {
  if (pref === "system") return media.matches ? "dark" : "light";
  return pref;
}

export function applyTheme(pref: ThemePreference): void {
  document.documentElement.classList.toggle("dark", resolveTheme(pref) === "dark");
}

export function onSystemThemeChange(fn: () => void): () => void {
  media.addEventListener("change", fn);
  return () => media.removeEventListener("change", fn);
}
