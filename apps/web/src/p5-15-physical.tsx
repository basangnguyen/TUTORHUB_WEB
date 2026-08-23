import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@tutorhub/design-tokens/tokens.css";
import "@tutorhub/ui/styles.css";
import "./styles.css";
import { I18nProvider } from "./app/i18n";
import { P515AccessibilityHarness } from "./features/collaboration/P515AccessibilityHarness";

const root = document.getElementById("root");
if (!root) throw new Error("p515_harness_root_missing");

createRoot(root).render(
  <StrictMode>
    <I18nProvider initialLanguage="en">
      <P515AccessibilityHarness />
    </I18nProvider>
  </StrictMode>,
);
