import { render, screen } from "@testing-library/react";
import { createElement } from "react";
import { describe, expect, it } from "vitest";
import { MarkdownRenderer } from "@/lib/markdown";

describe("MarkdownRenderer", () => {
  it("renders static Markdown content through the project adapter", () => {
    render(
      createElement(MarkdownRenderer, {
        content: "hello **world**",
      })
    );
    expect(screen.getByText("world")).toBeInTheDocument();
  });

  it("renders mention-aware content through the configured mention path", () => {
    render(
      createElement(MarkdownRenderer, {
        content:
          '<mention type="user" id="users/alice" name="alice">@alice</mention>',
        mentionAware: true,
      })
    );
    expect(screen.getByRole("button")).toHaveAttribute(
      "data-mid",
      "users/alice"
    );
  });
});
