import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// MentionPopup uses react-i18next via useTranslation; stub it so the test does
// not depend on the i18n provider and asserts structure, not copy.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

import type { MentionTarget } from "@/composables/useMentionTargets";
import { MentionPopup } from "./mention-popup";

const targets: MentionTarget[] = [
  { type: "user", id: "u1", name: "alice" },
  { type: "agent", id: "a1", name: "bot" },
  { type: "user", id: "u2", name: "bob" },
];

describe("MentionPopup a11y", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("TestMentionPopup_AriaRoles: listbox + option roles with aria-selected on the active option", () => {
    render(
      <MentionPopup
        id="mention-popup"
        targets={targets}
        query="a"
        position={{ top: 100, left: 10, height: 20 }}
        selectedIndex={1}
        onSelect={() => {}}
        onClose={() => {}}
      />
    );

    const listbox = screen.getByRole("listbox");
    expect(listbox).toHaveAttribute("id", "mention-popup");

    const options = screen.getAllByRole("option");
    expect(options).toHaveLength(3);

    // selectedIndex=1 points at the second target ("bot", the agent), since
    // the popup lays out users first then agents.
    const active = options[1];
    expect(active).toHaveAttribute("aria-selected", "true");
    expect(active.id).toBe("mention-popup-opt-1");

    // All non-active options report aria-selected=false.
    for (const option of [options[0], options[2]]) {
      expect(option).toHaveAttribute("aria-selected", "false");
    }
  });
});
