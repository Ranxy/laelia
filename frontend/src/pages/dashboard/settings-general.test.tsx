import { create } from "@bufbuild/protobuf";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import {
  WorkspaceProfileSetting,
  WorkspaceProfileSettingSchema,
} from "@/types/proto-es/store/setting_pb";
import {
  SettingSchema,
  SettingValueSchema,
  UpdateSettingRequest,
} from "@/types/proto-es/v1/setting_pb";
import { SettingsGeneralPage } from "./settings-general";

// --- mock @/connect so the page's setting RPCs are controllable ---
const mock = vi.hoisted(() => ({
  getSetting: vi.fn(),
  updateSetting: vi.fn(),
}));

vi.mock("@/connect", () => ({
  settingServiceClient: {
    getSetting: mock.getSetting,
    updateSetting: mock.updateSetting,
  },
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

const EMAIL_VERIFICATION_LABEL = "settings.general.require-email-verification";
const EMAIL_VERIFICATION_PATH =
  "value.workspace_profile.require_email_verification";

type ProfileOverrides = Omit<
  Partial<WorkspaceProfileSetting>,
  "$typeName" | "$unknown"
>;

function profile(overrides?: ProfileOverrides): WorkspaceProfileSetting {
  return create(WorkspaceProfileSettingSchema, {
    externalUrl: "https://example.com",
    disallowSignup: false,
    requireEmailVerification: true,
    enforceIdentityDomain: false,
    domains: [],
    ...overrides,
  });
}

function settingResponse(value: WorkspaceProfileSetting) {
  return create(SettingSchema, {
    name: "settings/workspace_profile",
    value: create(SettingValueSchema, {
      value: { case: "workspaceProfile", value },
    }),
  });
}

function renderPage() {
  return render(<SettingsGeneralPage />);
}

// rowSwitch finds the switch inside the settings row whose heading matches
// the i18n key (the t mock returns keys verbatim).
function rowSwitch(labelKey: string) {
  const heading = screen.getByText(labelKey);
  const row = heading.closest(
    ".flex.items-center.justify-between"
  ) as HTMLElement;
  return within(row).getByRole("switch");
}

beforeEach(() => {
  useAppStore.setState({
    workspaceProfile: undefined,
    smtpConfig: undefined,
    s3Config: undefined,
    llmAgentConfig: undefined,
    userMcpConfig: undefined,
    passwordRestriction: undefined,
  });
  mock.getSetting.mockReset();
  mock.updateSetting.mockReset();
  toastMock.add.mockReset();
});

describe("settings-general", () => {
  it("renders the signup and email-verification switches from the profile", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(profile()));

    renderPage();

    const signup = await waitFor(() =>
      rowSwitch("settings.general.allow-signup")
    );
    expect(signup).toBeChecked();
    expect(rowSwitch(EMAIL_VERIFICATION_LABEL)).toBeChecked();
  });

  it("sends a field-level update for only the email-verification path when toggled off", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(profile()));
    mock.updateSetting.mockResolvedValue(
      settingResponse(profile({ requireEmailVerification: false }))
    );

    renderPage();
    const sw = await waitFor(() => rowSwitch(EMAIL_VERIFICATION_LABEL));
    fireEvent.click(sw);

    await waitFor(() => expect(mock.updateSetting).toHaveBeenCalledTimes(1));
    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.updateMask?.paths).toEqual([EMAIL_VERIFICATION_PATH]);
    const sent = req.setting?.value?.value?.value as
      | WorkspaceProfileSetting
      | undefined;
    expect(sent?.requireEmailVerification).toBe(false);
    // Unrelated fields round-trip from the cached profile untouched.
    expect(sent?.disallowSignup).toBe(false);
  });

  it("keeps the switch off after a successful save (no bounce-back)", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(profile()));
    mock.updateSetting.mockResolvedValue(
      settingResponse(profile({ requireEmailVerification: false }))
    );

    renderPage();
    const sw = await waitFor(() => rowSwitch(EMAIL_VERIFICATION_LABEL));
    fireEvent.click(sw);

    await waitFor(() => expect(mock.updateSetting).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(sw).not.toBeChecked());
  });

  it("rolls the switch back and toasts an error when the save fails", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(profile()));
    mock.updateSetting.mockRejectedValue(new Error("boom"));

    renderPage();
    const sw = await waitFor(() => rowSwitch(EMAIL_VERIFICATION_LABEL));
    fireEvent.click(sw);

    await waitFor(() => expect(sw).toBeChecked());
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "error",
        title: "settings.general.save-failed",
      })
    );
  });

  it("shows the persisted value on remount (refresh keeps the toggle off)", async () => {
    mock.getSetting.mockResolvedValue(
      settingResponse(profile({ requireEmailVerification: false }))
    );

    const { unmount } = renderPage();
    await waitFor(() => rowSwitch(EMAIL_VERIFICATION_LABEL));
    unmount();

    renderPage();
    const sw = await waitFor(() => rowSwitch(EMAIL_VERIFICATION_LABEL));
    expect(sw).not.toBeChecked();
  });

  it("hides the email-verification switch when signup is disabled", async () => {
    mock.getSetting.mockResolvedValue(
      settingResponse(profile({ disallowSignup: true }))
    );

    renderPage();

    await waitFor(() => rowSwitch("settings.general.allow-signup"));
    expect(
      screen.queryByText(EMAIL_VERIFICATION_LABEL)
    ).not.toBeInTheDocument();
  });
});
