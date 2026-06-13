import type { RouteObject } from "react-router-dom";
import { SplashLayout } from "@/react/app/layouts/splash-layout";

export const authRoutes: RouteObject[] = [
  {
    path: "auth",
    element: <SplashLayout />,
    children: [
      {
        path: "signin",
        handle: { name: "auth.signin" },
        lazy: () =>
          import("@/react/pages/auth/signin").then((m) => ({
            Component: m.SignInPage,
          })),
      },
      {
        path: "signup",
        handle: { name: "auth.signup" },
        lazy: () =>
          import("@/react/pages/auth/signup").then((m) => ({
            Component: m.SignUpPage,
          })),
      },
    ],
  },
];
