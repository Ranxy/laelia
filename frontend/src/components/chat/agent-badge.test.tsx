import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// AgentBadge uses react-i18next directly (no provider in tests); return the
// key so assertions read deterministically.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

import { AgentBadge } from "./agent-badge";

describe("AgentBadge", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("renders a text-only agent label without an icon", () => {
    const { container } = render(<AgentBadge />);

    expect(screen.getByText("chat.agent")).toBeInTheDocument();
    expect(container.querySelector("svg")).not.toBeInTheDocument();
  });
});
