import {
  createBrowserRouter,
  type RouteObject,
  redirect,
} from "react-router-dom";
import { RootLayout } from "@/app/root-layout";
import { RouterErrorBoundary } from "./error-boundary";
import { rootGuard } from "./guard";
import { buildRouteNameIndex, setRouteNameIndex } from "./route-index";
import { authRoutes } from "./routes/auth";
import { dashboardRoutes } from "./routes/dashboard";

const allRoutes: RouteObject[] = [
  {
    element: <RootLayout />,
    errorElement: <RouterErrorBoundary />,
    loader: ({ request }: { request: Request }) => {
      return rootGuard({ url: new URL(request.url) });
    },
    children: [
      ...authRoutes,
      ...dashboardRoutes,
      { path: "*", loader: () => redirect("/") },
    ],
  },
];

setRouteNameIndex(buildRouteNameIndex(allRoutes));

export const router = createBrowserRouter(allRoutes);
