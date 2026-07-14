import { readFileSync } from "node:fs";

const tokenFile = new URL(
  "../packages/design-tokens/src/tokens.css",
  import.meta.url,
);
const css = readFileSync(tokenFile, "utf8");

function parseBlock(selector) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = css.match(new RegExp(`${escaped}\\s*\\{([\\s\\S]*?)\\n\\}`));
  if (!match) {
    throw new Error(`Missing token block: ${selector}`);
  }

  return Object.fromEntries(
    [...match[1].matchAll(/(--[\w-]+):\s*(#[0-9a-f]{6})\s*;/gi)].map(
      ([, name, value]) => [name, value.toLowerCase()],
    ),
  );
}

function rgb(hex) {
  return [1, 3, 5].map((offset) =>
    Number.parseInt(hex.slice(offset, offset + 2), 16),
  );
}

function luminance(hex) {
  const channels = rgb(hex).map((value) => {
    const normalized = value / 255;
    return normalized <= 0.04045
      ? normalized / 12.92
      : ((normalized + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
}

function contrast(foreground, background) {
  const lighter = Math.max(luminance(foreground), luminance(background));
  const darker = Math.min(luminance(foreground), luminance(background));
  return (lighter + 0.05) / (darker + 0.05);
}

const light = parseBlock(":root");
const dark = { ...light, ...parseBlock('[data-theme="dark"]') };
const checks = [
  ["light body", light, "--color-text", "--color-surface", 4.5],
  ["light muted", light, "--color-text-muted", "--color-surface", 4.5],
  ["light primary", light, "--color-accent-contrast", "--color-accent", 4.5],
  ["light danger", light, "--color-danger", "--color-surface", 4.5],
  ["dark body", dark, "--color-text", "--color-surface", 4.5],
  ["dark muted", dark, "--color-text-muted", "--color-surface", 4.5],
  ["dark primary", dark, "--color-accent-contrast", "--color-accent", 4.5],
  ["dark danger", dark, "--color-danger", "--color-surface", 4.5],
];

const failures = [];
for (const [label, theme, foregroundName, backgroundName, minimum] of checks) {
  const foreground = theme[foregroundName];
  const background = theme[backgroundName];
  if (!foreground || !background) {
    failures.push(`${label}: missing ${foregroundName} or ${backgroundName}`);
    continue;
  }

  const ratio = contrast(foreground, background);
  if (ratio < minimum) {
    failures.push(`${label}: ${ratio.toFixed(2)}:1 is below ${minimum}:1`);
  }
}

if (failures.length > 0) {
  throw new Error(
    `Design token contrast check failed:\n${failures.join("\n")}`,
  );
}

console.log(`Design token contrast checks passed (${checks.length} pairs).`);
