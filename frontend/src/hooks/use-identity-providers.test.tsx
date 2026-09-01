import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { createElement } from "react";
import { describe, expect, it, vi } from "vitest";
import { useIdentityProviders } from "./use-identity-providers";

const mock = vi.hoisted(() => ({
  listIdentityProviders: vi.fn(),
}));

vi.mock("@/connect", () => ({
  identityProviderServiceClient: {
    listIdentityProviders: mock.listIdentityProviders,
  },
}));

function TestProbe() {
  const { providers, loaded, error } = useIdentityProviders();
  return createElement(
    "div",
    null,
    createElement(
      "div",
      { "data-testid": "providers" },
      JSON.stringify(providers.map((p) => p.name))
    ),
    createElement(
      "div",
      { "data-testid": "state" },
      `${loaded ? "loaded " : ""}${error ? "error" : ""}`
    )
  );
}

function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  render(
    createElement(QueryClientProvider, { client }, createElement(TestProbe))
  );
}

describe("useIdentityProviders", () => {
  it("exposes the configured providers once loaded", async () => {
    mock.listIdentityProviders.mockResolvedValue({
      identityProviders: [{ name: "idps/okta" }, { name: "idps/azure" }],
    });

    mount();

    await waitFor(() =>
      expect(screen.getByTestId("state").textContent).toContain("loaded")
    );
    expect(screen.getByTestId("providers").textContent).toBe(
      JSON.stringify(["idps/okta", "idps/azure"])
    );
    expect(screen.getByTestId("state").textContent).not.toContain("error");
  });

  it("marks the read as settled with an empty list on failure", async () => {
    mock.listIdentityProviders.mockRejectedValue(new Error("down"));

    mount();

    await waitFor(() =>
      expect(screen.getByTestId("state").textContent).toContain("error")
    );
    expect(screen.getByTestId("providers").textContent).toBe("[]");
  });
});
