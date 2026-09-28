import type { Config } from "./types";

/** Pushes the theme settings onto <html> as data attributes and CSS variables. */
export function applyTheme(config: Config) {
  const { theme } = config;
  const root = document.documentElement;
  root.dataset.mode = theme.mode;
  root.dataset.font = theme.font;
  root.dataset.cards = theme.cards;
  root.dataset.density = theme.density;
  root.style.setProperty("--accent", theme.accent);
  root.style.setProperty("--accent-rgb", hexToRgb(theme.accent));
  if (theme.background) {
    root.style.setProperty("--bg-image", `url("${theme.background.replace(/"/g, "%22")}")`);
    root.style.setProperty("--bg-dim", String(theme.background_dim));
    root.style.setProperty("--bg-blur", `${theme.background_blur}px`);
    root.dataset.background = "on";
  } else {
    delete root.dataset.background;
  }
  document.title = config.title;

  let style = document.getElementById("foyer-custom-css");
  if (!style) {
    style = document.createElement("style");
    style.id = "foyer-custom-css";
    document.head.appendChild(style);
  }
  style.textContent = theme.custom_css ?? "";

  const meta = document.querySelector('meta[name="theme-color"]');
  meta?.setAttribute("content", getComputedStyle(root).getPropertyValue("--bg").trim() || "#000");
}

function hexToRgb(hex: string): string {
  const n = parseInt(hex.slice(1), 16);
  return `${(n >> 16) & 255} ${(n >> 8) & 255} ${n & 255}`;
}
