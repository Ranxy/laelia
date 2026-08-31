import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TwoPaneShell } from "./two-pane-shell";

function renderShell({
  detailOpen,
  railMobileDirection,
}: {
  detailOpen: boolean;
  railMobileDirection?: "row" | "column";
}) {
  return render(
    <TwoPaneShell
      rail={<div data-testid="rail">rail</div>}
      detailOpen={detailOpen}
      width="w-72"
      railMobileDirection={railMobileDirection}
    >
      <div data-testid="pane">pane</div>
    </TwoPaneShell>
  );
}

describe("two-pane-shell", () => {
  it("without an open detail, the rail owns the mobile screen and the pane waits for lg", () => {
    renderShell({ detailOpen: false });

    const aside = screen.getByTestId("rail").closest("aside");
    const main = screen.getByTestId("pane").closest("main");

    expect(aside).toHaveClass("flex", "w-full");
    expect(aside).toHaveClass("lg:w-72");
    // The pane stays in the DOM for lg+ but is hidden below lg.
    expect(aside?.nextElementSibling).toBe(main);
    expect(main).toHaveClass("hidden", "lg:block", "min-w-0", "flex-1");
  });

  it("with an open detail, the rail hides on mobile and the pane takes over", () => {
    renderShell({ detailOpen: true });

    const aside = screen.getByTestId("rail").closest("aside");
    const main = screen.getByTestId("pane").closest("main");

    expect(aside).toHaveClass("hidden", "lg:flex");
    // Open state keeps the unprefixed width (the aside is display:none below
    // lg anyway); closed state's lg:w-72 variant is asserted in the prior test.
    expect(aside).toHaveClass("w-72");
    expect(main).not.toHaveClass("hidden");
    expect(main).toHaveClass("min-w-0", "flex-1");

    // Both panes stay mounted; switching is purely responsive css.
    expect(screen.getByTestId("rail")).toBeInTheDocument();
    expect(screen.getByTestId("pane")).toBeInTheDocument();
  });

  it("keeps the callers' rail direction differences explicit (column default, row for chat)", () => {
    const first = renderShell({ detailOpen: false });
    const columnAside = screen.getByTestId("rail").closest("aside");
    expect(columnAside).toHaveClass("flex-col");
    first.unmount();

    renderShell({ detailOpen: false, railMobileDirection: "row" });
    const rowAside = screen.getByTestId("rail").closest("aside");
    expect(rowAside).toHaveClass("lg:flex-col");
    expect(rowAside).not.toHaveClass("flex-col");
  });

  it("applies the callers' rail width and rail/pane extras", () => {
    render(
      <TwoPaneShell
        rail={<div data-testid="rail" />}
        detailOpen
        width="w-56"
        railClassName="bg-background overflow-hidden"
        className="overflow-hidden"
      >
        <div data-testid="pane" />
      </TwoPaneShell>
    );

    expect(screen.getByTestId("rail").closest("aside")).toHaveClass(
      "bg-background",
      "overflow-hidden"
    );
    expect(screen.getByTestId("pane").closest("main")).toHaveClass(
      "overflow-hidden"
    );
  });
});
