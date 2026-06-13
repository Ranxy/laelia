import { RouterProvider } from "react-router-dom";
import { Toaster } from "@/react/components/ui/toaster";
import { router } from "@/react/router";

export function AppRoot() {
  return (
    <>
      <RouterProvider router={router} />
      <Toaster />
    </>
  );
}
