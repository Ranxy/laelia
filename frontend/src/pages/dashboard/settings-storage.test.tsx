import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import {
  S3ConfigSetting,
  S3ConfigSettingSchema,
} from "@/types/proto-es/store/setting_pb";
import {
  SettingSchema,
  SettingValueSchema,
  UpdateSettingRequest,
} from "@/types/proto-es/v1/setting_pb";
import { SettingsStoragePage } from "./settings-storage";

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

type S3Overrides = Omit<Partial<S3ConfigSetting>, "$typeName" | "$unknown">;

function s3(overrides?: S3Overrides): S3ConfigSetting {
  return create(S3ConfigSettingSchema, {
    endpoint: "https://s3.example.com",
    region: "us-east-1",
    bucket: "laelia",
    accessKey: "AKIA",
    secretKey: "****secret",
    forcePathStyle: true,
    useSsl: true,
    ...overrides,
  });
}

function settingResponse(value: S3ConfigSetting) {
  return create(SettingSchema, {
    name: "settings/s3_config",
    value: create(SettingValueSchema, {
      value: { case: "s3Config", value },
    }),
  });
}

function renderPage() {
  return render(<SettingsStoragePage />);
}

beforeEach(() => {
  useAppStore.setState({ s3Config: undefined });
  mock.getSetting.mockReset();
  mock.updateSetting.mockReset();
  toastMock.add.mockReset();
});

describe("settings-storage", () => {
  it("loads and renders the stored config", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(s3()));

    renderPage();

    const endpoint = await screen.findByLabelText("settings.s3.endpoint");
    expect(endpoint).toHaveValue("https://s3.example.com");
    expect(screen.getByLabelText("settings.s3.region")).toHaveValue(
      "us-east-1"
    );
    expect(screen.getByLabelText("settings.s3.bucket")).toHaveValue("laelia");
    expect(screen.getByLabelText("settings.s3.access-key")).toHaveValue("AKIA");
    expect(
      screen.getByRole("checkbox", { name: "settings.s3.force-path-style" })
    ).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "settings.s3.use-ssl" })
    ).toBeChecked();
  });

  it("shows the masked-secret hint when the stored secret is masked", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(s3()));

    renderPage();

    await screen.findByLabelText("settings.s3.endpoint");
    expect(screen.getByText("settings.s3.secret-masked")).toBeInTheDocument();
  });

  it("saves edited fields with the full s3 update mask", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(s3()));
    mock.updateSetting.mockResolvedValue(
      settingResponse(s3({ bucket: "laelia2" }))
    );

    renderPage();
    const bucket = await screen.findByLabelText("settings.s3.bucket");
    fireEvent.change(bucket, { target: { value: "laelia2" } });
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => expect(mock.updateSetting).toHaveBeenCalledTimes(1));
    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.updateMask?.paths).toEqual([
      "value.s3_config.endpoint",
      "value.s3_config.region",
      "value.s3_config.bucket",
      "value.s3_config.access_key",
      "value.s3_config.secret_key",
      "value.s3_config.force_path_style",
      "value.s3_config.use_ssl",
    ]);
    const sent = req.setting?.value?.value?.value as
      | S3ConfigSetting
      | undefined;
    expect(sent?.bucket).toBe("laelia2");
    expect(sent?.forcePathStyle).toBe(true);
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success", title: "settings.s3.saved" })
    );
  });

  it("toasts an error when the save fails", async () => {
    mock.getSetting.mockResolvedValue(settingResponse(s3()));
    mock.updateSetting.mockRejectedValue(new Error("s3 down"));

    renderPage();
    await screen.findByLabelText("settings.s3.endpoint");
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() =>
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "error",
          title: "settings.s3.save-failed",
          description: "s3 down",
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
          title: "settings.s3.load-failed",
        })
      )
    );
  });
});
