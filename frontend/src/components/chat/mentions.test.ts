import { describe, expect, it } from "vitest";
import {
  contentWithMentionTags,
  type MentionRef,
  mentionTagMarkdown,
  splitByMentions,
} from "@/components/chat/mentions";

const alice: MentionRef = { type: "user", id: "alice-user-1", name: "alice" };
const bob: MentionRef = { type: "agent", id: "bob-agent-1", name: "bob" };

describe("mentionTagMarkdown", () => {
  it("emits a <mention> node carrying type/id/name as attributes", () => {
    expect(mentionTagMarkdown(alice)).toBe(
      '<mention type="user" id="alice-user-1" name="alice">@alice</mention>'
    );
  });

  it("HTML-escapes attribute values and text so special chars round-trip", () => {
    const ref: MentionRef = { type: "user", id: "users/u-7", name: 'A&B"<' };
    const tag = mentionTagMarkdown(ref);
    // Raw angle brackets/quotes/ampersands are escaped so the tag parses.
    expect(tag).toBe(
      '<mention type="user" id="users/u-7" name="A&amp;B&quot;&lt;">@A&amp;B&quot;&lt;</mention>'
    );
  });
});

describe("contentWithMentionTags", () => {
  it("returns the content unchanged when there are no mentions", () => {
    expect(contentWithMentionTags("hello @nobody", [])).toBe("hello @nobody");
  });

  it("rewrites a single mention to an inline <mention> node", () => {
    expect(contentWithMentionTags("Sure @alice, please review.", [alice])).toBe(
      'Sure <mention type="user" id="alice-user-1" name="alice">@alice</mention>, please review.'
    );
  });

  it("rewrites multiple mentions in order", () => {
    expect(
      contentWithMentionTags("Hey @alice, ping @bob too.", [alice, bob])
    ).toBe(
      'Hey <mention type="user" id="alice-user-1" name="alice">@alice</mention>, ping <mention type="agent" id="bob-agent-1" name="bob">@bob</mention> too.'
    );
  });

  it("preserves surrounding markdown", () => {
    expect(contentWithMentionTags("Visit **@alice** now.", [alice])).toBe(
      'Visit **<mention type="user" id="alice-user-1" name="alice">@alice</mention>** now.'
    );
  });
});

describe("splitByMentions parity", () => {
  it("places one mention segment per matched occurrence", () => {
    const segs = splitByMentions("a @alice b @bob c", [alice, bob]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["a ", "@alice", " b ", "@bob", " c"]);
  });
});

describe("mentionTagMarkdown with label", () => {
  it("emits a label attribute and renders the label as text", () => {
    expect(mentionTagMarkdown(alice, "Alice Lee")).toBe(
      '<mention type="user" id="alice-user-1" name="alice" label="Alice Lee">@Alice Lee</mention>'
    );
  });

  it("HTML-escapes the label", () => {
    expect(mentionTagMarkdown(alice, 'A&B"<')).toBe(
      '<mention type="user" id="alice-user-1" name="alice" label="A&amp;B&quot;&lt;">@A&amp;B&quot;&lt;</mention>'
    );
  });
});

// Regression tests for the bugs that previously left some @mentions rendered
// as plain text ("@someone" / "@xxx-agent-N") instead of badges:
//   1. repeated mention — the same member @mentioned several times was only
//      rendered once (the backend dedups to a single Mention, and the old
//      matcher tried each mention exactly once);
//   2. prefix handles — "ran-user-1" was matched inside "@ran-user-10", eating
//      the longer handle and leaving "0" as stray text;
//   3. missing leading boundary — a "@" inside an email local-part or a word
//      could be mistaken for a mention.
const user1: MentionRef = { type: "user", id: "u-1", name: "ran-user-1" };
const user10: MentionRef = { type: "user", id: "u-10", name: "ran-user-10" };

describe("splitByMentions repeated mentions", () => {
  it("renders every occurrence of a repeated handle, not just the first", () => {
    const segs = splitByMentions("@alice hi @alice again @alice", [alice]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["@alice", " hi ", "@alice", " again ", "@alice"]);
  });

  it("renders every occurrence through contentWithMentionTags too", () => {
    expect(contentWithMentionTags("@alice hi @alice", [alice])).toBe(
      '<mention type="user" id="alice-user-1" name="alice">@alice</mention> hi <mention type="user" id="alice-user-1" name="alice">@alice</mention>'
    );
  });
});

describe("splitByMentions prefix handles", () => {
  it("matches the longer handle when a shorter one is a prefix", () => {
    const segs = splitByMentions("ping @ran-user-10 then @ran-user-1", [
      user1,
      user10,
    ]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["ping ", "@ran-user-10", " then ", "@ran-user-1"]);
  });

  it("still matches the short handle when the long one is absent", () => {
    const segs = splitByMentions("ping @ran-user-1 ok", [user1, user10]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["ping ", "@ran-user-1", " ok"]);
  });
});

describe("splitByMentions boundaries", () => {
  it("does not match a @handle embedded in an email local-part", () => {
    const segs = splitByMentions("contact alice@x.com or @alice", [alice]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["contact alice@x.com or ", "@alice"]);
  });

  it("does not match a @handle preceded by a letter", () => {
    const segs = splitByMentions("foo@bar @alice", [
      { type: "user", id: "u-bar", name: "bar" },
      alice,
    ]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["foo@bar ", "@alice"]);
  });
});

// Regression: a trailing '.' (sentence-ending period) after a handle must not
// block mention rendering. '.' is a valid INTERNAL handle separator
// (team.lead-user-1) but a trailing '.' is punctuation. Before the fix the
// trailing boundary treated '.' as a handle continuation, so "@para-agent-1."
// at the end of a sentence never rendered as a badge even when the backend
// resolved the mention.
const paraAgent: MentionRef = {
  type: "agent",
  id: "agents/para",
  name: "para-agent-1",
};
const teamLead: MentionRef = {
  type: "user",
  id: "u-team",
  name: "team.lead-user-1",
};

describe("splitByMentions trailing sentence period", () => {
  it("renders a handle followed by a sentence period", () => {
    const segs = splitByMentions("Waiting for my role from @para-agent-1.", [
      paraAgent,
    ]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["Waiting for my role from ", "@para-agent-1", "."]);
  });

  it("renders a handle followed by a period and space", () => {
    const segs = splitByMentions("ping @para-agent-1. ok", [paraAgent]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["ping ", "@para-agent-1", ". ok"]);
  });

  it("still renders a handle followed by other punctuation (bang)", () => {
    const segs = splitByMentions("Looking forward to @para-agent-1!", [
      paraAgent,
    ]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["Looking forward to ", "@para-agent-1", "!"]);
  });

  it("preserves an internal dot and strips a trailing dot", () => {
    const segs = splitByMentions("ask @team.lead-user-1. please", [teamLead]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["ask ", "@team.lead-user-1", ". please"]);
  });

  it("renders a handle with an internal dot (no trailing dot)", () => {
    const segs = splitByMentions("escalate to @team.lead-user-1 now", [
      teamLead,
    ]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["escalate to ", "@team.lead-user-1", " now"]);
  });

  it("renders the handle through contentWithMentionTags too", () => {
    expect(
      contentWithMentionTags("Waiting for my role from @para-agent-1.", [
        paraAgent,
      ])
    ).toBe(
      'Waiting for my role from <mention type="agent" id="agents/para" name="para-agent-1">@para-agent-1</mention>.'
    );
  });
});

// A single backend Mention now carries both the canonical handle (id) and the
// display text (name). The frontend must match both forms so a message that
// was written with @handle still renders when name is the display name.
describe("splitByMentions handle + display-name from one Mention", () => {
  const jet: MentionRef = {
    type: "agent",
    id: "jet-agent-1",
    name: "jet",
  };

  it("matches the canonical handle from id", () => {
    const segs = splitByMentions("ping @jet-agent-1 now", [jet]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["ping ", "@jet", " now"]);
  });

  it("matches the display name from name", () => {
    const segs = splitByMentions("ping @jet now", [jet]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["ping ", "@jet", " now"]);
  });

  it("renders both forms through contentWithMentionTags", () => {
    expect(contentWithMentionTags("talk to @jet-agent-1 or @jet", [jet])).toBe(
      'talk to <mention type="agent" id="jet-agent-1" name="jet">@jet</mention> or <mention type="agent" id="jet-agent-1" name="jet">@jet</mention>'
    );
  });
});

// When the same message mentions two different members who share a display
// name, the backend sets name to the handle so badges stay unambiguous.
describe("splitByMentions ambiguous display names fall back to handles", () => {
  const jane1: MentionRef = {
    type: "agent",
    id: "jane-agent-1",
    name: "jane-agent-1",
  };
  const jane2: MentionRef = {
    type: "agent",
    id: "jane-agent-2",
    name: "jane-agent-2",
  };

  it("renders handles for same-named members", () => {
    const segs = splitByMentions("@jane-agent-1 and @jane-agent-2", [
      jane1,
      jane2,
    ]);
    expect(
      segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))
    ).toEqual(["@jane-agent-1", " and ", "@jane-agent-2"]);
  });
});
