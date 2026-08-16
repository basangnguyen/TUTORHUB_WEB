import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@tutorhub/design-tokens/tokens.css";
import "@tutorhub/ui/styles.css";
import "./styles.css";
import "./p4-11-physical.css";
import { I18nProvider } from "./app/i18n";
import { P411PhysicalHarness } from "./features/media/P411PhysicalHarness";

const root = document.getElementById("root");

if (!root) {
  throw new Error("P4-11 physical harness root is unavailable.");
}

createRoot(root).render(
  <StrictMode>
    <I18nProvider initialLanguage="vi">
      <P411PhysicalHarness />
    </I18nProvider>
  </StrictMode>,
);
