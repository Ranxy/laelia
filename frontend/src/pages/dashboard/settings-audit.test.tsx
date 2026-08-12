import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import type { AuditLog } from "@/types/proto-es/v1/audit_log_service_pb";
import { SettingsAuditPage } from "./settings-audit";

const mock = vi.hoisted(() => ({
  searchAuditLogs: vi.fn(),
  exportAuditLogs: vi.fn(),
}));

vi.mock("@/connect", () => ({
  auditLogServiceClient: {
    searchAuditLogs: mock.searchAuditLogs,
    exportAuditLogs: mock.exportAuditLogs,
  },
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

function log(
  name: string,
  method: string,
  status: string,
  payload?: string
): AuditLog {
  return {
    name,
    method,
    actorId: "users/1",
    status,
    resource: "settings/workspace_profile",
    payload,
    createTime: { seconds: 0n, nanos: 0 },
  } as unknown as AuditLog;
}

function renderPage() {
  return render(<SettingsAuditPage />);
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      title: "Admin",
      permissions: ["laelia.auditLogs.search"],
    } as never,
  });
  mock.searchAuditLogs.mockReset();
  mock.exportAuditLogs.mockReset();
  toastMock.add.mockReset();
});

describe("settings-audit", () => {
  it("shows the permission notice without the auditLogs.search permission", async () => {
    useAppStore.setState({
      currentUser: { name: "users/2", title: "User", permissions: [] } as never,
    });

    renderPage();

    expect(
      await screen.findByText("settings.audit.not-allowed")
    ).toBeInTheDocument();
    expect(mock.searchAuditLogs).not.toHaveBeenCalled();
  });

  it("renders audit log rows", async () => {
    mock.searchAuditLogs.mockResolvedValue({
      auditLogs: [
        log("audit/1", "laelia.settings.UpdateSetting", "ok", "{}"),
        log("audit/2", "laelia.users.DeleteUser", "denied"),
      ],
      nextPageToken: "",
    });

    renderPage();

    expect(
      await screen.findByText("laelia.settings.UpdateSetting")
    ).toBeInTheDocument();
    expect(screen.getByText("laelia.users.DeleteUser")).toBeInTheDocument();
    expect(screen.getAllByText("ok").length).toBe(1);
    expect(screen.getAllByText("denied").length).toBe(1);
  });

  it("shows the empty hint when there are no logs", async () => {
    mock.searchAuditLogs.mockResolvedValue({
      auditLogs: [],
      nextPageToken: "",
    });

    renderPage();

    expect(
      await screen.findByText("settings.audit.no-logs")
    ).toBeInTheDocument();
  });

  it("filters by method and actor via the Filter button", async () => {
    mock.searchAuditLogs.mockResolvedValue({
      auditLogs: [],
      nextPageToken: "",
    });

    renderPage();
    await screen.findByText("settings.audit.no-logs");
    fireEvent.change(
      screen.getByPlaceholderText("settings.audit.filter-method-placeholder"),
      { target: { value: "UpdateSetting" } }
    );
    fireEvent.change(
      screen.getByPlaceholderText("settings.audit.filter-actor-placeholder"),
      { target: { value: "users/1" } }
    );
    fireEvent.click(
      screen.getByRole("button", { name: "settings.audit.filter" })
    );

    await waitFor(() => expect(mock.searchAuditLogs).toHaveBeenCalledTimes(2));
    const req = mock.searchAuditLogs.mock.calls[1][0] as { filter: string };
    expect(req.filter).toBe('method = "UpdateSetting" && actor = "users/1"');
  });

  it("expands and collapses a log payload", async () => {
    mock.searchAuditLogs.mockResolvedValue({
      auditLogs: [
        log("audit/1", "laelia.settings.UpdateSetting", "ok", '{"a":1}'),
      ],
      nextPageToken: "",
    });

    renderPage();
    const show = await screen.findByRole("button", {
      name: "settings.audit.show-payload",
    });
    fireEvent.click(show);
    expect(screen.getByText('{"a":1}')).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "settings.audit.hide-payload" })
    );
    expect(screen.queryByText('{"a":1}')).not.toBeInTheDocument();
  });

  it("exports the filtered logs as CSV", async () => {
    mock.searchAuditLogs.mockResolvedValue({
      auditLogs: [],
      nextPageToken: "",
    });
    mock.exportAuditLogs.mockResolvedValue({ content: "time,method\n" });
    const createObjectURL = vi
      .spyOn(URL, "createObjectURL")
      .mockReturnValue("blob:fake");
    const revoke = vi
      .spyOn(URL, "revokeObjectURL")
      .mockImplementation(() => {});

    renderPage();
    await screen.findByText("settings.audit.no-logs");
    fireEvent.click(
      screen.getByRole("button", { name: "settings.audit.export" })
    );

    await waitFor(() => expect(mock.exportAuditLogs).toHaveBeenCalledTimes(1));
    expect(createObjectURL).toHaveBeenCalled();
    expect(revoke).toHaveBeenCalled();
    createObjectURL.mockRestore();
    revoke.mockRestore();
  });

  it("toasts an error when the search fails", async () => {
    mock.searchAuditLogs.mockRejectedValue(new Error("audit down"));

    renderPage();

    await waitFor(() =>
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "error",
          title: "settings.audit.load-failed",
          description: "audit down",
        })
      )
    );
  });
});
