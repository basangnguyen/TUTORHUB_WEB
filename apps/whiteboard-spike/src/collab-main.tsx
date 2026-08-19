import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { CollabApp } from "./collab/CollabApp";
import "./styles.css";

const root = document.getElementById("root");
if (!root) {
  throw new Error("Không tìm thấy #root.");
}

createRoot(root).render(
  <StrictMode>
    <CollabApp />
  </StrictMode>,
);
