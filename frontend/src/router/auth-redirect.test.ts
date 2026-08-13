import { describe, expect, it } from "vitest";
import { isAuthPath, isPublicPath, resolveAuthRedirect } from "./auth-redirect";

describe("resolveAuthRedirect", () => {
  it("does not redirect while the session is still loading", () => {
    expect(
      resolveAuthRedirect({
        sessionLoaded: false,
        isLoggedIn: false,
        pathname: "/agents/x/chat",
        search: "",
      })
    ).toBeNull();
  });

  it("redirects a logged-out user from a protected route to sign-in, preserving destination", () => {
    const target = resolveAuthRedirect({
      sessionLoaded: true,
      isLoggedIn: false,
      pathname: "/agents/x/chat",
      search: "?q=1",
    });
    expect(target).toBe(
      `/auth/signin?redirect=${encodeURIComponent("/agents/x/chat?q=1")}`
    );
  });

  it("allows a logged-out user to reach auth pages (no redirect)", () => {
    expect(
      resolveAuthRedirect({
        sessionLoaded: true,
        isLoggedIn: false,
        pathname: "/auth/signin",
        search: "",
      })
    ).toBeNull();
  });

  it("sends a logged-in user on an auth page to '/' when no redirect param", () => {
    expect(
      resolveAuthRedirect({
        sessionLoaded: true,
        isLoggedIn: true,
        pathname: "/auth/signin",
        search: "",
      })
    ).toBe("/");
  });

  it("honors the redirect param for a logged-in user on an auth page", () => {
    expect(
      resolveAuthRedirect({
        sessionLoaded: true,
        isLoggedIn: true,
        pathname: "/auth/signin",
        search: "?redirect=/agents/y/chat",
      })
    ).toBe("/agents/y/chat");
  });

  it("lets a logged-in user stay on a protected route", () => {
    expect(
      resolveAuthRedirect({
        sessionLoaded: true,
        isLoggedIn: true,
        pathname: "/agents/x/chat",
        search: "",
      })
    ).toBeNull();
  });

  it("lets a logged-out user reach the public device-login page", () => {
    expect(
      resolveAuthRedirect({
        sessionLoaded: true,
        isLoggedIn: false,
        pathname: "/login/device",
        search: "?user_code=0RBM-CK57",
      })
    ).toBeNull();
  });

  it("does not bounce a logged-in user off the device-login page", () => {
    expect(
      resolveAuthRedirect({
        sessionLoaded: true,
        isLoggedIn: true,
        pathname: "/login/device",
        search: "?user_code=0RBM-CK57",
      })
    ).toBeNull();
  });
});

describe("isAuthPath", () => {
  it("recognizes auth paths", () => {
    expect(isAuthPath("/auth/signin")).toBe(true);
    expect(isAuthPath("/auth/signup")).toBe(true);
  });

  it("rejects non-auth paths", () => {
    expect(isAuthPath("/agents/x/chat")).toBe(false);
    expect(isAuthPath("/")).toBe(false);
  });
});

describe("isPublicPath", () => {
  it("recognizes the device-login path", () => {
    expect(isPublicPath("/login/device")).toBe(true);
    expect(isPublicPath("/login/device?user_code=XXXX-XXXX")).toBe(true);
  });

  it("rejects other paths", () => {
    expect(isPublicPath("/auth/signin")).toBe(false);
    expect(isPublicPath("/machines")).toBe(false);
  });
});
