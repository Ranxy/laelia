import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import { GetWorkspaceInfoResponseSchema } from "@/types/proto-es/v1/setting_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { SignInPage } from "./signin";

const mock = vi.hoisted(() => ({
  getWorkspaceInfo: vi.fn(),
  login: vi.fn(),
  getCurrentUser: vi.fn(),
}));

vi.mock("@/connect", () => ({
  settingServiceClient: { getWorkspaceInfo: mock.getWorkspaceInfo },
  authServiceClient: { login: mock.login },
  userServiceClient: { getCurrentUser: mock.getCurrentUser },
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

const EMAIL = "alice@example.com";
const PASSWORD = "secret123";

function renderPage(initialEntry = "/auth/signin") {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/auth/signin" element={<SignInPage />} />
        <Route path="/" element={<div data-testid="home" />} />
        <Route path="/auth/signup" element={<div data-testid="signup" />} />
      </Routes>
    </MemoryRouter>
  );
}

async function fillAndSubmit() {
  fireEvent.change(screen.getByLabelText(/common\.email/), {
    target: { value: EMAIL },
  });
  fireEvent.change(screen.getByLabelText(/common\.password/), {
    target: { value: PASSWORD },
  });
  fireEvent.click(screen.getByRole("button", { name: "common.sign-in" }));
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: null,
    isLoggedIn: false,
    sessionLoaded: true,
  });
  mock.getWorkspaceInfo.mockReset();
  mock.login.mockReset();
  mock.getCurrentUser.mockReset();
  toastMock.add.mockReset();
});

describe("sign-in", () => {
  it("submits credentials, seeds the session and navigates to the redirect target", async () => {
    mock.getWorkspaceInfo.mockResolvedValue(
      create(GetWorkspaceInfoResponseSchema, { disallowSignup: false })
    );
    mock.login.mockResolvedValue({
      user: create(UserSchema, { name: "users/1", email: EMAIL }),
    });
    mock.getCurrentUser.mockResolvedValue(
      create(UserSchema, {
        name: "users/1",
        email: EMAIL,
        permissions: ["laelia.settings.get"],
      })
    );

    renderPage("/auth/signin?redirect=/members");
    await fillAndSubmit();

    await waitFor(() => expect(mock.login).toHaveBeenCalledTimes(1));
    const req = mock.login.mock.calls[0][0] as {
      email: string;
      password: string;
    };
    expect(req.email).toBe(EMAIL);
    expect(req.password).toBe(PASSWORD);
    await waitFor(() =>
      expect(useAppStore.getState().currentUser?.email).toBe(EMAIL)
    );
    expect(useAppStore.getState().isLoggedIn).toBe(true);
  });

  it("keeps the submit button disabled until both fields are filled", () => {
    mock.getWorkspaceInfo.mockResolvedValue(
      create(GetWorkspaceInfoResponseSchema, { disallowSignup: false })
    );
    renderPage();

    const submit = screen.getByRole("button", { name: "common.sign-in" });
    expect(submit).toBeDisabled();
    fireEvent.change(screen.getByLabelText(/common\.email/), {
      target: { value: EMAIL },
    });
    expect(submit).toBeDisabled();
    fireEvent.change(screen.getByLabelText(/common\.password/), {
      target: { value: PASSWORD },
    });
    expect(submit).toBeEnabled();
  });

  it("shows an error toast when login fails", async () => {
    mock.getWorkspaceInfo.mockResolvedValue(
      create(GetWorkspaceInfoResponseSchema, { disallowSignup: false })
    );
    mock.login.mockRejectedValue(new Error("bad credentials"));

    renderPage();
    await fillAndSubmit();

    await waitFor(() =>
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "error",
          title: "auth.sign-in.failed",
          description: "bad credentials",
        })
      )
    );
  });

  it("toggles password visibility", () => {
    mock.getWorkspaceInfo.mockResolvedValue(
      create(GetWorkspaceInfoResponseSchema, { disallowSignup: false })
    );
    renderPage();

    const password = screen.getByLabelText(/common\.password/);
    expect(password).toHaveAttribute("type", "password");
    fireEvent.click(
      screen.getByRole("button", { name: "common.toggle-password-visibility" })
    );
    expect(password).toHaveAttribute("type", "text");
  });

  it("hides the signup link when the workspace disallows signup", async () => {
    mock.getWorkspaceInfo.mockResolvedValue(
      create(GetWorkspaceInfoResponseSchema, { disallowSignup: true })
    );
    renderPage();

    await waitFor(() =>
      expect(screen.queryByText("common.sign-up")).not.toBeInTheDocument()
    );
  });

  it("shows the signup link and navigates to signup with the redirect preserved", async () => {
    mock.getWorkspaceInfo.mockResolvedValue(
      create(GetWorkspaceInfoResponseSchema, { disallowSignup: false })
    );
    renderPage("/auth/signin?redirect=/members");

    const link = await screen.findByText("common.sign-up");
    fireEvent.click(link);
    expect(screen.getByTestId("signup")).toBeInTheDocument();
  });
});
