import { describe, expect, it } from "vitest";
import {
  type MentionTarget,
  mentionLabelResolver,
} from "@/composables/useMentionTargets";

const targets: MentionTarget[] = [
  { type: "user", id: "u-1", handle: "ran-user-1", name: "Ran" },
  { type: "user", id: "u-2", handle: "ran-user-2", name: "Ran" },
  { type: "agent", id: "agents/a-9", handle: "rei-agent-1", name: "Rei" },
];

describe("mentionLabelResolver", () => {
  it("returns the display name for unique names", () => {
    const label = mentionLabelResolver(targets);
    expect(label("rei-agent-1")).toBe("Rei");
  });

  it("disambiguates same-named members as name(handle)", () => {
    const label = mentionLabelResolver(targets);
    expect(label("ran-user-1")).toBe("Ran(ran-user-1)");
    expect(label("ran-user-2")).toBe("Ran(ran-user-2)");
  });

  it("returns undefined for handles not in the roster", () => {
    const label = mentionLabelResolver(targets);
    expect(label("ghost-user-1")).toBeUndefined();
  });

  it("treats an empty roster as no labels", () => {
    const label = mentionLabelResolver([]);
    expect(label("ran-user-1")).toBeUndefined();
  });
});
