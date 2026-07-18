import "@testing-library/jest-dom/vitest";

for (const [name, implementation] of [
  ["hasPointerCapture", () => false],
  ["releasePointerCapture", () => undefined],
  ["setPointerCapture", () => undefined],
  ["scrollIntoView", () => undefined],
] as const) {
  if (!(name in HTMLElement.prototype)) {
    Object.defineProperty(HTMLElement.prototype, name, {
      configurable: true,
      value: implementation,
    });
  }
}
