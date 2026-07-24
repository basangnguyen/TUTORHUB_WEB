import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";

const root = document.getElementById("root");
if (!root) {
  throw new Error("Không tìm thấy #root.");
}

document.body.dataset.calendarStartedAt = String(performance.now());

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
