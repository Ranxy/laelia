import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
// Load i18n before first render so translations are available
import "@/lib/i18n";
import { AppRoot } from "@/app/root";
import "@/assets/css/tailwind.css";
import "markstream-react/index.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AppRoot />
  </StrictMode>
);
