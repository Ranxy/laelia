import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { SidePanel } from "./side-panel";

const { isDesktopState } = vi.hoisted(() => ({
  isDesktopState: { value: true },
}));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (k: string) => k }),
}));

vi.mock("@/lib/use-is-desktop", () => ({
  useIsDesktop: () => isDesktopState.value,
}));

describe("SidePanel inline shell", () => {
  it("renders header (icon/title/meta), body and footer in place", () => {
    render(
      <SidePanel
        label="panel.label"
        icon={<span data-testid="icon" />}
        title="Panel title"
        meta="12:00"
        onClose={() => {}}
      >
        <p>body content</p>
      </SidePanel>
    );

    expect(
      screen.getByRole("complementary", { name: "panel.label" })
    ).toBeInTheDocument();
    expect(screen.getByTestId("icon")).toBeInTheDocument();
    expect(screen.getByText("Panel title")).toBeInTheDocument();
    expect(screen.getByText("12:00")).toBeInTheDocument();
    expect(screen.getByText("body content")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "common.close" })
    ).toBeInTheDocument();
  });

  it("omits the close button when the host owns the toggle", () => {
    render(
      <SidePanel label="panel.label" title="Panel title">
        <p>body content</p>
      </SidePanel>
    );

    expect(screen.queryByRole("button", { name: "common.close" })).toBeNull();
  });

  it("renders the pinned footer below the scrollable body", () => {
    render(
      <SidePanel
        label="panel.label"
        footer={<button type="button">send</button>}
      >
        <p>body content</p>
      </SidePanel>
    );

    expect(screen.getByRole("button", { name: "send" })).toBeInTheDocument();
    expect(screen.getByText("body content")).toBeInTheDocument();
  });
});

describe("SidePanel mobile sheet", () => {
  it("presents as a right-edge sheet on mobile when mobileSheet is set", () => {
    isDesktopState.value = false;
    try {
      const onClose = vi.fn();
      render(
        <SidePanel
          label="panel.label"
          title="Panel title"
          onClose={onClose}
          mobileSheet
        >
          <p>body content</p>
        </SidePanel>
      );

      // The drawer is a dialog with an accessible (sr-only) title.
      expect(screen.getByRole("dialog")).toBeInTheDocument();
      expect(screen.getByText("panel.label")).toBeInTheDocument();
      expect(screen.getByText("body content")).toBeInTheDocument();

      fireEvent.click(screen.getByRole("button", { name: "common.close" }));
      expect(onClose).toHaveBeenCalled();
    } finally {
      isDesktopState.value = true;
    }
  });

  it("keeps the in-place aside on desktop even with mobileSheet", () => {
    render(
      <SidePanel label="panel.label" title="Panel title" mobileSheet>
        <p>body content</p>
      </SidePanel>
    );

    expect(
      screen.getByRole("complementary", { name: "panel.label" })
    ).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
