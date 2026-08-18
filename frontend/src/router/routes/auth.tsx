import type { RouteObject } from "react-router-dom";
import { SplashLayout } from "@/app/layouts/splash-layout";

export const authRoutes: RouteObject[] = [
  {
    path: "auth",
    element: <SplashLayout />,
    children: [
      {
        path: "signin",
        handle: { name: "auth.signin" },
        lazy: () =>
          import("@/pages/auth/signin").then((m) => ({
            Component: m.SignInPage,
          })),
      },
      {
        path: "signup",
        handle: { name: "auth.signup" },
        lazy: () =>
          import("@/pages/auth/signup").then((m) => ({
            Component: m.SignUpPage,
          })),
      },
      {
        path: "verify-email",
        handle: { name: "auth.verify-email" },
        lazy: () =>
          import("@/pages/auth/verify-email").then((m) => ({
            Component: m.VerifyEmailPage,
          })),
      },
    ],
  },
  {
    // Public device-login approval page. It lives outside /auth so the URL
    // printed by `laelia-machine setup` is short; the auth guard exempts it
    // in both directions (see router/auth-redirect.ts isPublicPath).
    path: "login/device",
    element: <SplashLayout />,
    handle: { name: "auth.device-login" },
    lazy: () =>
      import("@/pages/auth/device-login").then((m) => ({
        Component: m.DeviceLoginPage,
      })),
  },
  {
    // OAuth2 SSO callback. The provider redirects here after the user
    // authorizes; this page exchanges the code for a session via AuthService.
    path: "oauth/callback",
    element: <SplashLayout />,
    handle: { name: "auth.oauth-callback" },
    lazy: () =>
      import("@/pages/auth/oauth-callback").then((m) => ({
        Component: m.OAuthCallbackPage,
      })),
  },
  {
    // Public deep-link entry point: /oauth/login/{providerId} starts the OAuth
    // flow for a specific provider without clicking the sign-in page button.
    path: "oauth/login/:providerId",
    element: <SplashLayout />,
    handle: { name: "auth.oauth-login" },
    lazy: () =>
      import("@/pages/auth/oauth-login").then((m) => ({
        Component: m.OAuthLoginPage,
      })),
  },
];
