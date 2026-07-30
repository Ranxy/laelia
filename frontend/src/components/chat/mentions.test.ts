import { describe, expect, it } from "vitest";
import {
  contentWithMentionTags,
  mentionTagMarkdown,
  splitByMentions,
  type MentionRef,
} from "@/components/chat/mentions";

const alice: MentionRef = { type: "user", id: "u-1", name: "alice" };
const bob: MentionRef = { type: "agent", id: "agents/a-9", name: "bob" };

describe("mentionTagMarkdown", () => {
  it("emits a <mention> node carrying type/id/name as attributes", () => {
    expect(mentionTagMarkdown(alice)).toBe(
      '<mention type="user" id="u-1" name="alice">@alice</mention>'
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
      'Sure <mention type="user" id="u-1" name="alice">@alice</mention>, please review.'
    );
  });

  it("rewrites multiple mentions in order", () => {
    expect(
      contentWithMentionTags("Hey @alice, ping @bob too.", [alice, bob])
    ).toBe(
      'Hey <mention type="user" id="u-1" name="alice">@alice</mention>, ping <mention type="agent" id="agents/a-9" name="bob">@bob</mention> too.'
    );
  });

  it("preserves surrounding markdown", () => {
    expect(contentWithMentionTags("Visit **@alice** now.", [alice])).toBe(
      'Visit **<mention type="user" id="u-1" name="alice">@alice</mention>** now.'
    );
  });
});

describe("splitByMentions parity", () => {
  it("places one mention segment per matched occurrence", () => {
    const segs = splitByMentions("a @alice b @bob c", [alice, bob]);
    expect(segs.map((s) => (s.mention ? `@${s.mention.name}` : s.text))).toEqual([
      "a ",
      "@alice",
      " b ",
      "@bob",
      " c",
    ]);
  });
});