import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { CollabApp } from "./collab/CollabApp";
import { LoadHarness } from "./collab/LoadHarness";
import "./styles.css";

const root = document.getElementById("root");
if (!root) {
  throw new Error("Không tìm thấy #root.");
}

const mode = new URLSearchParams(window.location.search).get("mode");

createRoot(root).render(
  <StrictMode>
    {mode === "collab" ? (
      <CollabApp />
    ) : mode === "load" ? (
      <LoadHarness />
    ) : (
      <App />
    )}
  </StrictMode>,
);
