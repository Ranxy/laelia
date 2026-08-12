import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import {
  SMTPSetting,
  SMTPSettingSchema,
} from "@/types/proto-es/store/setting_pb";
import {
  SettingSchema,
  SettingValueSchema,
  UpdateSettingRequest,
} from "@/types/proto-es/v1/setting_pb";
import { SettingsSmtpPage } from "./settings-smtp";

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

type SmtpOverrides = Omit<Partial<SMTPSetting>, "$typeName" | "$unknown">;

function smtp(overrides?: SmtpOverrides): SMTPSetting {
  return create(SMTPSettingSchema, {
    host: "smtp.example.com",
    port: 587,
    username: "mailer",
    password: "****secret",
    from: "noreply@example.com",
    useTls: true,
    ...overrides,
  });
}

function settingResponse(value: SMTPSetting) {
  return create(SettingSchema, {
    name: "settings/smtp_config",
    value: create(SettingValueSchema, {
      value: { case: "smtpConfig", value },
    }),
  });
}

function renderPage() {
  return render(<SettingsSmtpPage />);
}

beforeEach(() => {
  useAppStore.setState({ smtpConfig: undefined });
  mock.getSetting.mockReset();
  mock.updateSetting.mockReset();
  toastMock.add.mockReset();
});

describe("settings-smtp", () => {
  it("loads and renders the stored config", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(smtp()));

    renderPage();

    const host = await screen.findByLabelText("settings.smtp.host");
    expect(host).toHaveValue("smtp.example.com");
    expect(screen.getByLabelText("settings.smtp.port")).toHaveValue(587);
    expect(screen.getByLabelText("settings.smtp.username")).toHaveValue(
      "mailer"
    );
    expect(screen.getByLabelText("settings.smtp.from")).toHaveValue(
      "noreply@example.com"
    );
    expect(
      screen.getByRole("checkbox", { name: "settings.smtp.use-tls" })
    ).toBeChecked();
  });

  it("shows the masked-password hint when the stored secret is masked", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(smtp()));

    renderPage();

    await screen.findByLabelText("settings.smtp.host");
    expect(
      screen.getByText("settings.smtp.password-masked")
    ).toBeInTheDocument();
  });

  it("saves edited fields with the full smtp update mask", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(smtp()));
    mock.updateSetting.mockResolvedValue(
      settingResponse(smtp({ host: "smtp2.example.com" }))
    );

    renderPage();
    const host = await screen.findByLabelText("settings.smtp.host");
    fireEvent.change(host, { target: { value: "smtp2.example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => expect(mock.updateSetting).toHaveBeenCalledTimes(1));
    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.updateMask?.paths).toEqual([
      "value.smtp_config.host",
      "value.smtp_config.port",
      "value.smtp_config.username",
      "value.smtp_config.password",
      "value.smtp_config.from",
      "value.smtp_config.use_tls",
    ]);
    const sent = req.setting?.value?.value?.value as SMTPSetting | undefined;
    expect(sent?.host).toBe("smtp2.example.com");
    expect(sent?.port).toBe(587);
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success", title: "settings.smtp.saved" })
    );
  });

  it("toasts an error when the save fails", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(smtp()));
    mock.updateSetting.mockRejectedValue(new Error("smtp down"));

    renderPage();
    await screen.findByLabelText("settings.smtp.host");
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() =>
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "error",
          title: "settings.smtp.save-failed",
          description: "smtp down",
        })
      )
    );
  });

  it("toasts an error when loading the config fails", async () => {
    mock.getSetting.mockRejectedValue(new Error("boom"));

    renderPage();

    await waitFor(() =>
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "error",
          title: "settings.smtp.load-failed",
        })
      )
    );
  });
});
