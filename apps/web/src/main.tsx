import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@tutorhub/design-tokens/tokens.css";
import "@tutorhub/ui/styles.css";
import "./styles.css";
import { App } from "./App";
import { primePublicAvailabilityPollToken } from "./app/publicAvailabilityPollToken";

// Capability fragments must be erased before React can mount any query.
primePublicAvailabilityPollToken();

const root = document.getElementById("root");

if (!root) {
  throw new Error("Không tìm thấy phần tử #root.");
}

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
