import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MentionBadge } from "./mention-badge";

describe("MentionBadge a11y", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("TestMentionBadge_KeyboardActivatable: activates on Enter and Space", () => {
    const onClick = vi.fn();
    render(<MentionBadge name="alice" onClick={onClick} />);

    const badge = screen.getByRole("button", { name: "@alice" });
    expect(badge).toHaveAttribute("tabindex", "0");

    badge.focus();
    expect(document.activeElement).toBe(badge);

    // Enter activates the mention.
    fireEvent.keyDown(badge, { key: "Enter" });
    expect(onClick).toHaveBeenCalledTimes(1);

    // Space activates the mention (and does not scroll the page).
    fireEvent.keyDown(badge, { key: " " });
    expect(onClick).toHaveBeenCalledTimes(2);
  });
});
