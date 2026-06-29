import { RouterProvider } from "react-router-dom";
import { Toaster } from "@/components/ui/toaster";
import { router } from "@/router";

export function AppRoot() {
  return (
    <>
      <RouterProvider router={router} />
      <Toaster />
    </>
  );
}
