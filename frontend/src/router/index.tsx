import {
  createBrowserRouter,
  type RouteObject,
  redirect,
} from "react-router-dom";
import { RootLayout } from "@/react/app/root-layout";
import { rootGuard } from "./guard";
import { authRoutes } from "./routes/auth";
import { dashboardRoutes } from "./routes/dashboard";

const allRoutes: RouteObject[] = [
  {
    element: <RootLayout />,
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

export const router = createBrowserRouter(allRoutes);
