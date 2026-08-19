import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { ExcalidrawCandidateApp } from "./excalidraw/ExcalidrawCandidateApp";
import "./styles.css";

const root = document.getElementById("root");

if (!root) {
  throw new Error("Không tìm thấy root cho Excalidraw candidate.");
}

createRoot(root).render(
  <StrictMode>
    <ExcalidrawCandidateApp />
  </StrictMode>,
);
