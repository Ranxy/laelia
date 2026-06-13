import type { RouteObject } from "react-router-dom";
import { DashboardLayout } from "@/react/app/layouts/dashboard-layout";

export const dashboardRoutes: RouteObject[] = [
  {
    element: <DashboardLayout />,
    children: [
      {
        index: true,
        handle: { name: "workspace.landing" },
        lazy: () =>
          import("@/react/pages/dashboard/landing").then((m) => ({
            Component: m.LandingPage,
          })),
      },
    ],
  },
];
