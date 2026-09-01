import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { useAppStore } from "./index";

// --- mock @/connect: the auth slice's two clients become controllable stubs ---
const mock = vi.hoisted(() => ({
  login: vi.fn(),
  logout: vi.fn(),
  verifyEmail: vi.fn(),
  resendVerificationEmail: vi.fn(),
  getCurrentUser: vi.fn(),
  createUser: vi.fn(),
}));

vi.mock("@/connect", () => ({
  authServiceClient: {
    login: mock.login,
    logout: mock.logout,
    verifyEmail: mock.verifyEmail,
    resendVerificationEmail: mock.resendVerificationEmail,
  },
  userServiceClient: {
    getCurrentUser: mock.getCurrentUser,
    createUser: mock.createUser,
  },
}));

const plainUser = create(UserSchema, { name: "users/1", email: "a@x.io" });
const enrichedUser = create(UserSchema, {
  name: "users/1",
  email: "a@x.io",
  permissions: ["laelia.settings.get"],
});

beforeEach(() => {
  useAppStore.setState({
    currentUser: null,
    isLoggedIn: false,
    sessionLoaded: false,
  });
  mock.login.mockReset();
  mock.logout.mockReset();
  mock.getCurrentUser.mockReset();
  mock.createUser.mockReset();
});

describe("auth slice", () => {
  it("seeds the session from the login response, then enriches it via GetCurrentUser", async () => {
    mock.login.mockResolvedValue({ user: plainUser });
    mock.getCurrentUser.mockResolvedValue(enrichedUser);

    await useAppStore.getState().login("a@x.io", "pw");

    expect(mock.login).toHaveBeenCalledTimes(1);
    expect(useAppStore.getState().isLoggedIn).toBe(true);
    // The enrichment fetch fills caller-scoped fields (permissions) in.
    expect(useAppStore.getState().currentUser?.permissions).toEqual([
      "laelia.settings.get",
    ]);
  });

  it("keeps the login-response user when the enrichment fetch fails", async () => {
    mock.login.mockResolvedValue({ user: plainUser });
    mock.getCurrentUser.mockRejectedValue(new Error("down"));

    await useAppStore.getState().login("a@x.io", "pw");

    expect(useAppStore.getState().isLoggedIn).toBe(true);
    expect(useAppStore.getState().currentUser?.email).toBe("a@x.io");
    expect(useAppStore.getState().currentUser?.permissions ?? []).toEqual([]);
  });

  it("routes IdP logins through the oauth2 context in the login payload", async () => {
    mock.login.mockResolvedValue({ user: plainUser });
    mock.getCurrentUser.mockResolvedValue(enrichedUser);

    await useAppStore
      .getState()
      .login("", "", { idpName: "idps/okta", code: "abc" });

    const req = mock.login.mock.calls[0][0];
    expect(req.email).toBe("");
    expect(req.web).toBe(true);
    expect(req.idpName).toBe("idps/okta");
    expect(req.idpContext.context.value.code).toBe("abc");
  });

  it("resets the store on logout but keeps sessionLoaded so the guard does not flash", async () => {
    mock.logout.mockResolvedValue({});
    useAppStore.setState({
      currentUser: plainUser,
      isLoggedIn: true,
      sessionLoaded: true,
    });

    await useAppStore.getState().logout();

    expect(useAppStore.getState().currentUser).toBeNull();
    expect(useAppStore.getState().isLoggedIn).toBe(false);
    expect(useAppStore.getState().sessionLoaded).toBe(true);
  });

  it("registers without auto-login, sending a USER-type principal", async () => {
    mock.createUser.mockResolvedValue({});

    await useAppStore.getState().register("a@x.io", "Alice", "pw123456");

    expect(mock.createUser).toHaveBeenCalledTimes(1);
    const req = mock.createUser.mock.calls[0][0];
    expect(req.user.email).toBe("a@x.io");
    expect(req.user.title).toBe("Alice");
    expect(req.user.userType).toBe(1);
    // No auto-login: verification may be pending (the signup page decides).
    expect(useAppStore.getState().isLoggedIn).toBe(false);
  });

  it("does not clobber an established session when a stale fetchCurrentUser fails", async () => {
    useAppStore.setState({ currentUser: plainUser, isLoggedIn: true });
    mock.getCurrentUser.mockRejectedValue(new Error("stale"));

    await useAppStore.getState().fetchCurrentUser();

    expect(useAppStore.getState().currentUser?.name).toBe("users/1");
    expect(useAppStore.getState().isLoggedIn).toBe(true);
  });

  it("clears an anonymous visitor when fetchCurrentUser fails", async () => {
    useAppStore.setState({ currentUser: null, isLoggedIn: false });
    mock.getCurrentUser.mockRejectedValue(new Error("anon"));

    await useAppStore.getState().fetchCurrentUser();

    expect(useAppStore.getState().currentUser).toBeNull();
    expect(useAppStore.getState().isLoggedIn).toBe(false);
  });

  it("marks the session loaded after loadSession regardless of outcome", async () => {
    useAppStore.setState({ sessionLoaded: false });
    mock.getCurrentUser.mockRejectedValue(new Error("not logged in"));

    await useAppStore.getState().loadSession();

    expect(useAppStore.getState().sessionLoaded).toBe(true);
    expect(useAppStore.getState().isLoggedIn).toBe(false);
  });
});
