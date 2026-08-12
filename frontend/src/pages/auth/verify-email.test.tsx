import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  ResendVerificationEmailRequest,
  VerifyEmailRequest,
} from "@/types/proto-es/v1/auth_service_pb";
import { VerifyEmailPage } from "./verify-email";

// --- mock @/connect so the verify page's RPCs are controllable ---
const mock = vi.hoisted(() => ({
  verifyEmail: vi.fn(),
  resendVerificationEmail: vi.fn(),
}));

vi.mock("@/connect", () => ({
  authServiceClient: {
    verifyEmail: mock.verifyEmail,
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

function renderPage(token?: string) {
  const entry =
    token === undefined
      ? "/auth/verify-email"
      : `/auth/verify-email?token=${token}`;
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/auth/verify-email" element={<VerifyEmailPage />} />
        <Route path="/auth/signin" element={<div data-testid="signin" />} />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  mock.verifyEmail.mockReset();
  mock.resendVerificationEmail.mockReset();
  toastMock.add.mockReset();
});

describe("verify-email", () => {
  it("shows success for a valid token and links to sign-in", async () => {
    mock.verifyEmail.mockResolvedValue({});

    renderPage("valid-token");

    expect(
      await screen.findByText("auth.verify-email.success")
    ).toBeInTheDocument();
    const verifyReq = mock.verifyEmail.mock.calls[0][0] as VerifyEmailRequest;
    expect(verifyReq.token).toBe("valid-token");

    fireEvent.click(screen.getByRole("button", { name: "common.sign-in" }));
    expect(await screen.findByTestId("signin")).toBeInTheDocument();
  });

  it("shows the failure state with a resend form for an invalid token", async () => {
    mock.verifyEmail.mockRejectedValue(new Error("bad token"));

    renderPage("expired-token");

    expect(
      await screen.findByText("auth.verify-email.failed")
    ).toBeInTheDocument();
    expect(screen.getByPlaceholderText("common.email")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "auth.verify-email.resend" })
    ).toBeInTheDocument();
  });

  it("resends the verification email from the failure state", async () => {
    mock.verifyEmail.mockRejectedValue(new Error("bad token"));
    mock.resendVerificationEmail.mockResolvedValue({});

    renderPage("expired-token");
    await screen.findByText("auth.verify-email.failed");

    fireEvent.change(screen.getByPlaceholderText("common.email"), {
      target: { value: "bob@example.com" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "auth.verify-email.resend" })
    );

    await waitFor(() =>
      expect(mock.resendVerificationEmail).toHaveBeenCalledTimes(1)
    );
    const resendReq = mock.resendVerificationEmail.mock
      .calls[0][0] as ResendVerificationEmailRequest;
    expect(resendReq.email).toBe("bob@example.com");
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "success",
        title: "auth.verify-email.resend-sent",
      })
    );
  });

  it("shows the failure state without calling verify when no token is present", async () => {
    renderPage();

    expect(
      await screen.findByText("auth.verify-email.failed")
    ).toBeInTheDocument();
    expect(mock.verifyEmail).not.toHaveBeenCalled();
  });
});
