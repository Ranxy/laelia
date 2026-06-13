import { redirect } from "react-router-dom";
import { useAppStore } from "@/react/stores";

export function rootGuard({ url }: { url: URL }): Response | null {
  const isAuthPath = url.pathname.startsWith("/auth/");
  if (isAuthPath) return null; // RootLayout handles auth-page redirects reactively

  const { isLoggedIn, sessionLoaded } = useAppStore.getState();
  // Don't redirect until session is loaded (avoids flash to signin)
  if (!sessionLoaded) return null;

  if (!isLoggedIn) {
    const redirectTo = encodeURIComponent(url.pathname + url.search);
    return redirect(`/auth/signin?redirect=${redirectTo}`);
  }

  return null;
}
