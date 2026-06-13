import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { AppRoot } from "@/react/app/root";
import "@/react/assets/css/tailwind.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AppRoot />
  </StrictMode>
);
