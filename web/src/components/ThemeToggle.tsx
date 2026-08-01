// 主题切换器只操作公开主题偏好，不参与会话和旅行状态。
import { Moon, Sun } from "@phosphor-icons/react";
import { useState } from "react";
import { saveTheme, type Theme } from "../lib/theme";

function currentTheme(): Theme {
  return document.documentElement.dataset.theme === "dark" ? "dark" : "light";
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(currentTheme);
  const nextTheme = theme === "dark" ? "light" : "dark";
  const label = nextTheme === "dark" ? "切换至深色模式" : "切换至浅色模式";
  const Icon = theme === "dark" ? Sun : Moon;

  return (
    <button
      className="theme-toggle"
      type="button"
      aria-label={label}
      title={label}
      onClick={() => {
        saveTheme(nextTheme);
        setTheme(nextTheme);
      }}
    >
      <Icon aria-hidden="true" size={18} weight="bold" />
      <span>{theme === "dark" ? "浅色" : "深色"}</span>
    </button>
  );
}
