/**
 * Auth redirect resolution, shared by the root route loader (`rootGuard`) and
 * the reactive `RootLayout` effect so the two paths can never disagree.
 *
 * The root loader only runs on navigation, so it cannot react to the auth store
 * flipping `sessionLoaded`/`isLoggedIn` after the initial load. `RootLayout`
 * re-evaluates this on every store change, which is what makes the guard
 * "reactive". Keeping the decision in one pure function makes it unit-testable.
 */
export interface AuthRedirectInput {
  sessionLoaded: boolean;
  isLoggedIn: boolean;
  pathname: string;
  search: string;
}

export function isAuthPath(pathname: string): boolean {
  return pathname.startsWith("/auth/");
}

/**
 * Guards a user-supplied `redirect` target. Only same-app absolute paths are
 * honored; anything else (empty, external URL, protocol-relative `//evil.com`)
 * falls back to `/`. SPA navigation never leaves the site today, but every
 * consumer of the `redirect` param must pass through this so a future
 * `window.location.assign` call site cannot turn the param into an open
 * redirect.
 */
export function sanitizeRedirect(raw: string | null | undefined): string {
  if (!raw || !raw.startsWith("/") || raw.startsWith("//")) {
    return "/";
  }
  return raw;
}

// isPublicPath reports whether a route is reachable by both logged-in and
// logged-out users. The device-login approval page (/login/device) is public:
// a logged-out user must be able to open the URL from the machine's terminal
// and sign in there, while a logged-in user must not be bounced off it.
export function isPublicPath(pathname: string): boolean {
  return (
    pathname.startsWith("/login/device") ||
    pathname.startsWith("/oauth/callback") ||
    pathname.startsWith("/oauth/login")
  );
}

/**
 * Returns the target URL to navigate to, or `null` when no redirect is needed.
 *
 * - While the session is still loading, never redirect (avoids a flash to
 *   sign-in before we know whether the user is logged in).
 * - A logged-out user on a protected route is sent to sign-in, preserving the
 *   intended destination in the `redirect` query param.
 * - A logged-in user on an auth page (e.g. `/auth/signin`) is sent to the
 *   `redirect` query param, or `/` if absent.
 */
export function resolveAuthRedirect(input: AuthRedirectInput): string | null {
  const { sessionLoaded, isLoggedIn, pathname, search } = input;
  if (!sessionLoaded) {
    return null;
  }

  // Public routes are exempt from the guard in both directions.
  if (isPublicPath(pathname)) {
    return null;
  }

  const onAuth = isAuthPath(pathname);

  if (isLoggedIn) {
    if (!onAuth) {
      return null;
    }
    const params = new URLSearchParams(search);
    return sanitizeRedirect(params.get("redirect"));
  }

  // Logged out: auth pages are reachable, everything else redirects to sign-in.
  if (onAuth) {
    return null;
  }
  const redirect = encodeURIComponent(pathname + search);
  return `/auth/signin?redirect=${redirect}`;
}
