import { useSyncExternalStore } from "react";

// The console remembers light or dark per browser; dark is the default. The root layout applies it before paint.
export type Theme = "light" | "dark";

const key = "hookbeam.theme";

export function currentTheme(): Theme {
  if (typeof document === "undefined") return "dark";
  return document.documentElement.classList.contains("dark") ? "dark" : "light";
}

export function applyTheme(theme: Theme) {
  document.documentElement.classList.toggle("dark", theme === "dark");
  document.documentElement.style.colorScheme = theme;
  try {
    localStorage.setItem(key, theme);
  } catch {
    // Private windows may refuse storage; the choice then lasts for the page.
  }
}

function subscribe(onChange: () => void) {
  const observer = new MutationObserver(onChange);
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
  return () => observer.disconnect();
}

// useTheme follows the class on <html>, so widgets that paint themselves (diagrams, the API reference) switch
// with the toggle.
export function useTheme(): Theme {
  return useSyncExternalStore(subscribe, currentTheme, () => "dark");
}
