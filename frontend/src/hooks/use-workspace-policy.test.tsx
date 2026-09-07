import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { createElement } from "react";
import { describe, expect, it, vi } from "vitest";
import { useWorkspacePolicy } from "./use-workspace-policy";

const mock = vi.hoisted(() => ({
  getWorkspaceInfo: vi.fn(),
}));

vi.mock("@/connect", () => ({
  settingServiceClient: { getWorkspaceInfo: mock.getWorkspaceInfo },
}));

function TestProbe() {
  const policy = useWorkspacePolicy();
  return createElement(
    "div",
    { "data-testid": "policy" },
    JSON.stringify(policy)
  );
}

function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  return render(
    createElement(QueryClientProvider, { client }, createElement(TestProbe))
  );
}

describe("useWorkspacePolicy", () => {
  it("maps the response fields into the semantic policy shape", async () => {
    mock.getWorkspaceInfo.mockResolvedValue({
      disallowSignup: true,
      requireEmailVerification: true,
      enforceIdentityDomain: true,
      domains: ["example.com"],
      disallowUserCreateMachine: true,
      allowCustomImages: true,
    });

    mount();

    await waitFor(() =>
      expect(screen.getByTestId("policy").textContent).toContain(
        '"signupDisallowed":true'
      )
    );
    expect(JSON.parse(screen.getByTestId("policy").textContent ?? "")).toEqual({
      signupDisallowed: true,
      requireEmailVerification: true,
      enforceIdentityDomain: true,
      allowedDomains: ["example.com"],
      userCreateMachineDisallowed: true,
      allowCustomImages: true,
    });
  });

  it("keeps the domain list empty when identity domains are not enforced", async () => {
    mock.getWorkspaceInfo.mockResolvedValue({
      disallowSignup: false,
      requireEmailVerification: false,
      enforceIdentityDomain: false,
      domains: ["ignored.example.com"],
      disallowUserCreateMachine: false,
    });

    mount();

    await waitFor(() =>
      expect(screen.getByTestId("policy").textContent).toContain(
        '"enforceIdentityDomain":false'
      )
    );
    const policy = JSON.parse(screen.getByTestId("policy").textContent ?? "");
    expect(policy.allowedDomains).toEqual([]);
  });

  it("renders the permissive defaults while the read is in flight or failed", async () => {
    // In flight: the probe paints the defaults before the promise settles.
    mock.getWorkspaceInfo.mockReturnValue(new Promise(() => {}));
    const inFlight = mount();
    expect(JSON.parse(screen.getByTestId("policy").textContent ?? "")).toEqual({
      signupDisallowed: false,
      requireEmailVerification: false,
      enforceIdentityDomain: false,
      allowedDomains: [],
      userCreateMachineDisallowed: false,
      // Permissive default: the create-machine custom-image field stays
      // visible until the policy arrives (the backend enforces the switch).
      allowCustomImages: true,
    });
    inFlight.unmount();

    // Failure keeps the same defaults (the backend enforces the policy
    // server-side, so a failed read must not block the auth flows).
    mock.getWorkspaceInfo.mockRejectedValue(new Error("down"));
    mount();
    await waitFor(() =>
      expect(screen.getByTestId("policy").textContent).toContain(
        '"signupDisallowed":false'
      )
    );
  });
});
