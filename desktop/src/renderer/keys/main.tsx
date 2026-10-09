import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import KeysApp from "./KeysApp";
import "../styles/globals.css";

const root = document.getElementById("root");
if (!root) throw new Error("keys.html is missing #root");

createRoot(root).render(
  <StrictMode>
    <KeysApp />
  </StrictMode>,
);
