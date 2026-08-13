import { useMemo } from "react";
import type { MentionTarget } from "./useMentionTargets";

export interface MentionState {
  active: boolean;
  query: string;
  startIndex: number;
  matched: MentionTarget[];
}

export function detectMention(
  text: string,
  cursorPos: number,
  targets: MentionTarget[]
): MentionState | null {
  if (!text || cursorPos === 0) return null;

  let atIndex = -1;
  for (let i = cursorPos - 1; i >= 0; i--) {
    if (text[i] === "@") {
      atIndex = i;
      break;
    }
    if (text[i] === " " || text[i] === "\n") break;
  }

  if (atIndex === -1) return null;

  const query = text.slice(atIndex + 1, cursorPos);

  // Matching is handle-only: the message content only ever carries @<handle>,
  // so the popup filters by handle (display names never participate).
  const queryLower = query.toLowerCase();
  const matched = targets
    .filter((t) => t.handle.toLowerCase().includes(queryLower))
    .slice(0, 8);

  if (matched.length === 0) {
    return { active: true, query, startIndex: atIndex, matched: [] };
  }

  return { active: true, query, startIndex: atIndex, matched };
}

export function useMentionDetect(
  text: string,
  cursorPos: number,
  targets: MentionTarget[]
): MentionState | null {
  return useMemo(
    () => detectMention(text, cursorPos, targets),
    [text, cursorPos, targets]
  );
}
