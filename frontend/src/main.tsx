import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
// Load i18n before first render so translations are available
import "@/react/lib/i18n";
import { AppRoot } from "@/react/app/root";
import "@/react/assets/css/tailwind.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AppRoot />
  </StrictMode>
);
