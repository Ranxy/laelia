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
const EXTERNAL_URL_LABEL = "settings.general.external-url";
const EXTERNAL_URL_PATH = "value.workspace_profile.external_url";
const USER_CREATE_MACHINE_LABEL = "settings.general.allow-user-create-machine";
const USER_CREATE_MACHINE_PATH =
  "value.workspace_profile.disallow_user_create_machine";

type ProfileOverrides = Omit<
  Partial<WorkspaceProfileSetting>,
  "$typeName" | "$unknown"
>;

function profile(overrides?: ProfileOverrides): WorkspaceProfileSetting {
  return create(WorkspaceProfileSettingSchema, {
    externalUrl: "https://example.com",
    disallowSignup: false,
    requireEmailVerification: false,
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

// card finds the settings card containing the labelled field.
function card(labelKey: string) {
  const label = screen.getByLabelText(labelKey);
  return label.closest(
    ".rounded-lg.border.border-control-border.bg-background.p-5.shadow-xs"
  ) as HTMLElement;
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
    expect(rowSwitch(EMAIL_VERIFICATION_LABEL)).not.toBeChecked();
  });

  it("sends a field-level update for only the email-verification path when toggled off", async () => {
    mock.getSetting.mockResolvedValue(
      settingResponse(profile({ requireEmailVerification: true }))
    );
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
    mock.getSetting.mockResolvedValue(
      settingResponse(profile({ requireEmailVerification: true }))
    );
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

  it("shows the loading spinner while the profile is being fetched", () => {
    mock.getSetting.mockReturnValue(new Promise(() => {}));

    const { container } = renderPage();

    expect(container.querySelector("svg.animate-spin")).toBeInTheDocument();
  });

  it("toasts an error when the profile fails to load", async () => {
    mock.getSetting.mockRejectedValue(new Error("network down"));

    renderPage();

    await waitFor(
      () =>
        expect(toastMock.add).toHaveBeenCalledWith(
          expect.objectContaining({
            type: "error",
            title: "settings.general.load-failed",
          })
        ),
      { timeout: 3000 }
    );
  });

  it("sends a field-level update for only the disallow-signup path when toggled", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(profile()));
    mock.updateSetting.mockResolvedValue(
      settingResponse(profile({ disallowSignup: true }))
    );

    renderPage();
    const sw = await waitFor(() => rowSwitch("settings.general.allow-signup"));
    fireEvent.click(sw);

    await waitFor(() => expect(mock.updateSetting).toHaveBeenCalledTimes(1));
    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.updateMask?.paths).toEqual([
      "value.workspace_profile.disallow_signup",
    ]);
    const sent = req.setting?.value?.value?.value as
      | WorkspaceProfileSetting
      | undefined;
    expect(sent?.disallowSignup).toBe(true);
  });

  it("rolls the signup switch back and toasts when the save fails", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(profile()));
    mock.updateSetting.mockRejectedValue(new Error("boom"));

    renderPage();
    const sw = await waitFor(() => rowSwitch("settings.general.allow-signup"));
    fireEvent.click(sw);

    await waitFor(() => expect(sw).toBeChecked());
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "error",
        title: "settings.general.save-failed",
      })
    );
  });

  it("renders the user-created machine switch from the profile", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(profile()));

    renderPage();

    const sw = await waitFor(() => rowSwitch(USER_CREATE_MACHINE_LABEL));
    expect(sw).toBeChecked();
  });

  it("sends a field-level update for only the disallow-user-create-machine path when toggled", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(profile()));
    mock.updateSetting.mockResolvedValue(
      settingResponse(profile({ disallowUserCreateMachine: true }))
    );

    renderPage();
    const sw = await waitFor(() => rowSwitch(USER_CREATE_MACHINE_LABEL));
    fireEvent.click(sw);

    await waitFor(() => expect(mock.updateSetting).toHaveBeenCalledTimes(1));
    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.updateMask?.paths).toEqual([USER_CREATE_MACHINE_PATH]);
    const sent = req.setting?.value?.value?.value as
      | WorkspaceProfileSetting
      | undefined;
    expect(sent?.disallowUserCreateMachine).toBe(true);
  });

  it("rolls the user-created machine switch back and toasts when the save fails", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(profile()));
    mock.updateSetting.mockRejectedValue(new Error("boom"));

    renderPage();
    const sw = await waitFor(() => rowSwitch(USER_CREATE_MACHINE_LABEL));
    fireEvent.click(sw);

    await waitFor(() => expect(sw).toBeChecked());
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "error",
        title: "settings.general.save-failed",
      })
    );
  });

  it("toggles identity-domain enforcement and reveals the domains editor", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(profile()));
    mock.updateSetting.mockResolvedValue(
      settingResponse(profile({ enforceIdentityDomain: true }))
    );

    renderPage();
    const sw = await waitFor(() =>
      rowSwitch("settings.general.enforce-domain")
    );
    fireEvent.click(sw);

    await waitFor(() => expect(mock.updateSetting).toHaveBeenCalledTimes(1));
    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.updateMask?.paths).toEqual([
      "value.workspace_profile.enforce_identity_domain",
    ]);
    expect(
      await screen.findByLabelText(
        "settings.general.domains",
        {},
        { timeout: 3000 }
      )
    ).toBeInTheDocument();
  });

  it("saves the parsed domains list", async () => {
    mock.getSetting.mockResolvedValue(
      settingResponse(profile({ enforceIdentityDomain: true, domains: [] }))
    );
    mock.updateSetting.mockResolvedValue(
      settingResponse(
        profile({
          enforceIdentityDomain: true,
          domains: ["example.com", "foo.com"],
        })
      )
    );

    renderPage();
    const textarea = await screen.findByLabelText(
      "settings.general.domains",
      {},
      { timeout: 3000 }
    );
    fireEvent.change(textarea, {
      target: { value: "Example.com\n@foo.com\n\n" },
    });
    fireEvent.click(
      within(card("settings.general.domains")).getByRole("button", {
        name: "common.save",
      })
    );

    await waitFor(() => expect(mock.updateSetting).toHaveBeenCalledTimes(1));
    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.updateMask?.paths).toEqual(["value.workspace_profile.domains"]);
    const sent = req.setting?.value?.value?.value as
      | WorkspaceProfileSetting
      | undefined;
    expect(sent?.domains).toEqual(["example.com", "foo.com"]);
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "success",
        title: "settings.general.saved",
      })
    );
  });

  it("keeps the domains save button disabled until the list changes", async () => {
    mock.getSetting.mockResolvedValue(
      settingResponse(
        profile({ enforceIdentityDomain: true, domains: ["example.com"] })
      )
    );

    renderPage();
    await screen.findByLabelText(
      "settings.general.domains",
      {},
      { timeout: 3000 }
    );
    expect(
      within(card("settings.general.domains")).getByRole("button", {
        name: "common.save",
      })
    ).toBeDisabled();
  });

  it("renders the external URL from the profile", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(profile()));

    renderPage();

    const input = await screen.findByLabelText(EXTERNAL_URL_LABEL);
    expect(input).toHaveValue("https://example.com");
  });

  it("sends a field-level update for only the external-url path when saved", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(profile()));
    mock.updateSetting.mockResolvedValue(
      settingResponse(profile({ externalUrl: "https://new.example.com" }))
    );

    renderPage();
    const input = await screen.findByLabelText(EXTERNAL_URL_LABEL);
    fireEvent.change(input, {
      target: { value: "  https://new.example.com  " },
    });
    fireEvent.click(
      within(card(EXTERNAL_URL_LABEL)).getByRole("button", {
        name: "common.save",
      })
    );

    await waitFor(() => expect(mock.updateSetting).toHaveBeenCalledTimes(1));
    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.updateMask?.paths).toEqual([EXTERNAL_URL_PATH]);
    const sent = req.setting?.value?.value?.value as
      | WorkspaceProfileSetting
      | undefined;
    expect(sent?.externalUrl).toBe("https://new.example.com");
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "success",
        title: "settings.general.saved",
      })
    );
  });

  it("keeps the external URL save button disabled until the value changes", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(profile()));

    renderPage();
    await screen.findByLabelText(EXTERNAL_URL_LABEL);
    expect(
      within(card(EXTERNAL_URL_LABEL)).getByRole("button", {
        name: "common.save",
      })
    ).toBeDisabled();
  });
});
