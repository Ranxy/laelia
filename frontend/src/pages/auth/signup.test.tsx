import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import {
  LoginRequest,
  ResendVerificationEmailRequest,
} from "@/types/proto-es/v1/auth_service_pb";
import {
  GetWorkspaceInfoResponse,
  GetWorkspaceInfoResponseSchema,
} from "@/types/proto-es/v1/setting_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { SignUpPage } from "./signup";

// --- mock @/connect so the signup flow's RPCs are controllable ---
const mock = vi.hoisted(() => ({
  getWorkspaceInfo: vi.fn(),
  createUser: vi.fn(),
  login: vi.fn(),
  getCurrentUser: vi.fn(),
  resendVerificationEmail: vi.fn(),
}));

vi.mock("@/connect", () => ({
  settingServiceClient: { getWorkspaceInfo: mock.getWorkspaceInfo },
  userServiceClient: {
    createUser: mock.createUser,
    getCurrentUser: mock.getCurrentUser,
  },
  authServiceClient: {
    login: mock.login,
    resendVerificationEmail: mock.resendVerificationEmail,
  },
}));

// Stable t reference: a fresh arrow function per render would change the
// useTranslation identity and re-run effects that depend on it.
const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

const EMAIL = "alice@example.com";
const PASSWORD = "abc12345";

function workspaceInfo(overrides?: Partial<GetWorkspaceInfoResponse>) {
  return create(GetWorkspaceInfoResponseSchema, {
    disallowSignup: false,
    enforceIdentityDomain: false,
    domains: [],
    requireEmailVerification: true,
    ...overrides,
  });
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/auth/signup"]}>
      <Routes>
        <Route path="/auth/signup" element={<SignUpPage />} />
        <Route path="/" element={<div data-testid="home" />} />
      </Routes>
    </MemoryRouter>
  );
}

async function fillAndSubmit() {
  fireEvent.change(screen.getByLabelText(/common\.email/), {
    target: { value: EMAIL },
  });
  fireEvent.change(screen.getByLabelText(/common\.name/), {
    target: { value: "Alice" },
  });
  fireEvent.change(screen.getByLabelText(/common\.password/), {
    target: { value: PASSWORD },
  });
  fireEvent.change(screen.getByLabelText(/auth\.sign-up\.password-confirm/), {
    target: { value: PASSWORD },
  });
  fireEvent.click(screen.getByRole("button", { name: "common.sign-up" }));
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: null,
    isLoggedIn: false,
    sessionLoaded: false,
  });
  mock.getWorkspaceInfo.mockReset();
  mock.createUser.mockReset();
  mock.login.mockReset();
  mock.getCurrentUser.mockReset();
  mock.resendVerificationEmail.mockReset();
  toastMock.add.mockReset();
});

describe("signup", () => {
  it("registers and auto-logs-in when verification is off", async () => {
    mock.getWorkspaceInfo.mockResolvedValue(
      workspaceInfo({ requireEmailVerification: false })
    );
    mock.createUser.mockResolvedValue({});
    mock.login.mockResolvedValue({
      user: create(UserSchema, { email: EMAIL, title: "Alice" }),
    });
    mock.getCurrentUser.mockResolvedValue(
      create(UserSchema, { email: EMAIL, title: "Alice" })
    );

    renderPage();
    await screen.findByLabelText(/common\.email/);
    fillAndSubmit();

    await waitFor(() => expect(mock.createUser).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(mock.login).toHaveBeenCalledTimes(1));
    const loginReq = mock.login.mock.calls[0][0] as LoginRequest;
    expect(loginReq.email).toBe(EMAIL);
    expect(loginReq.password).toBe(PASSWORD);
    expect(await screen.findByTestId("home")).toBeInTheDocument();
  });

  it("registers without login and shows the check-your-inbox state when verification is on", async () => {
    mock.getWorkspaceInfo.mockResolvedValue(
      workspaceInfo({ requireEmailVerification: true })
    );
    mock.createUser.mockResolvedValue({});

    renderPage();
    await screen.findByLabelText(/common\.email/);
    fillAndSubmit();

    await waitFor(() => expect(mock.createUser).toHaveBeenCalledTimes(1));
    expect(mock.login).not.toHaveBeenCalled();
    expect(
      await screen.findByText("auth.sign-up.verify-sent")
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "auth.sign-up.resend" })
    ).toBeInTheDocument();
  });

  it("resends the verification email from the check-your-inbox state", async () => {
    mock.getWorkspaceInfo.mockResolvedValue(
      workspaceInfo({ requireEmailVerification: true })
    );
    mock.createUser.mockResolvedValue({});
    mock.resendVerificationEmail.mockResolvedValue({});

    renderPage();
    await screen.findByLabelText(/common\.email/);
    fillAndSubmit();
    fireEvent.click(
      await screen.findByRole("button", { name: "auth.sign-up.resend" })
    );

    await waitFor(() =>
      expect(mock.resendVerificationEmail).toHaveBeenCalledTimes(1)
    );
    const resendReq = mock.resendVerificationEmail.mock
      .calls[0][0] as ResendVerificationEmailRequest;
    expect(resendReq.email).toBe(EMAIL);
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "success",
        title: "auth.sign-up.resend-sent",
      })
    );
  });

  it("toasts an error and never logs in when registration fails", async () => {
    mock.getWorkspaceInfo.mockResolvedValue(
      workspaceInfo({ requireEmailVerification: false })
    );
    mock.createUser.mockRejectedValue(new Error("boom"));

    renderPage();
    await screen.findByLabelText(/common\.email/);
    fillAndSubmit();

    await waitFor(() =>
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "error",
          title: "auth.sign-up.failed",
        })
      )
    );
    expect(mock.login).not.toHaveBeenCalled();
  });

  it("renders password policy hints for weak passwords", async () => {
    mock.getWorkspaceInfo.mockResolvedValue(
      workspaceInfo({ requireEmailVerification: true })
    );

    renderPage();
    const password = await screen.findByLabelText(/common\.password/);
    fireEvent.focus(password);
    fireEvent.change(password, { target: { value: "abc" } });

    expect(
      await screen.findByText("auth.sign-up.password-min-length")
    ).toBeInTheDocument();
  });

  it("shows the disallowed notice instead of the form when signup is disabled", async () => {
    mock.getWorkspaceInfo.mockResolvedValue(
      workspaceInfo({ disallowSignup: true })
    );

    renderPage();

    expect(
      await screen.findByText("auth.sign-up.disallowed")
    ).toBeInTheDocument();
    expect(screen.queryByLabelText(/common\.email/)).not.toBeInTheDocument();
  });
});
