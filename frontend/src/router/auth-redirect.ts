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

  const onAuth = isAuthPath(pathname);

  if (isLoggedIn) {
    if (!onAuth) {
      return null;
    }
    const params = new URLSearchParams(search);
    return params.get("redirect") ?? "/";
  }

  // Logged out: auth pages are reachable, everything else redirects to sign-in.
  if (onAuth) {
    return null;
  }
  const redirect = encodeURIComponent(pathname + search);
  return `/auth/signin?redirect=${redirect}`;
}
