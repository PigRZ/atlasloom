// 主题工具只在浏览器本地持久化 light/dark 偏好。
export type Theme = "light" | "dark";

const storageKey = "my-travel-theme";

function preferredTheme(): Theme {
  const stored = window.localStorage.getItem(storageKey);
  if (stored === "light" || stored === "dark") return stored;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
  document.querySelector('meta[name="theme-color"]')?.setAttribute(
    "content",
    theme === "dark" ? "#111713" : "#f1f4f0"
  );
}

export function initializeTheme(): Theme {
  const theme = preferredTheme();
  applyTheme(theme);
  return theme;
}

export function saveTheme(theme: Theme): void {
  window.localStorage.setItem(storageKey, theme);
  applyTheme(theme);
}
