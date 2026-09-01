import { useEffect, useRef, useState } from "react";
import { userServiceClient } from "@/connect";
import { buildUserFilter } from "@/lib/user-filter";
import type { User } from "@/types/proto-es/v1/user_service_pb";

// ---------------------------------------------------------------------------
// useUserSearch — the debounced server-side user search (09 章 §2.2 收敛).
//
// Single home for the listUsers({filter: buildUserFilter(q)}) keystroke
// search that used to be copied into MemberPicker and FromSenderPicker.
//
// Semantics (divergence points made explicit):
//   - `searching` flips on with the query change itself (the FromSenderPicker
//     behavior), not after the 250ms debounce fires (MemberPicker's old
//     behavior): the dropdown shows the loading state during the debounce
//     window instead of flashing stale rows.
//   - The empty-query behavior is the caller's choice via `enabled`: pickers
//     that want a browseable first page keep it enabled (MemberPicker), the
//     ones that only search on typed input gate it off (FromSenderPicker).
//     While disabled the hook clears results so a closed picker never shows
//     stale rows.
//
// A generation counter guards the write-back: a response from a superseded
// keystroke (in flight when the next debounce fires) is dropped instead of
// clobbering the newer results — a race both hand-rolled copies shared.
// ---------------------------------------------------------------------------

const DEBOUNCE_MS = 250;
const PAGE_SIZE = 50;

export interface UseUserSearchOptions {
  enabled: boolean;
  pageSize?: number;
}

export interface UserSearchResult {
  results: User[];
  searching: boolean;
}

export function useUserSearch(
  query: string,
  opts: UseUserSearchOptions
): UserSearchResult {
  const { enabled, pageSize = PAGE_SIZE } = opts;
  const [results, setResults] = useState<User[]>([]);
  const [searching, setSearching] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const generationRef = useRef(0);

  useEffect(() => {
    if (!enabled) {
      generationRef.current += 1;
      if (debounceRef.current) clearTimeout(debounceRef.current);
      setResults([]);
      setSearching(false);
      return;
    }
    if (debounceRef.current) clearTimeout(debounceRef.current);
    setSearching(true);
    const generation = generationRef.current + 1;
    generationRef.current = generation;
    debounceRef.current = setTimeout(async () => {
      try {
        const res = await userServiceClient.listUsers({
          pageSize,
          filter: buildUserFilter(query),
        });
        if (generation !== generationRef.current) return;
        setResults(res.users ?? []);
      } catch {
        if (generation !== generationRef.current) return;
        setResults([]);
      } finally {
        if (generation === generationRef.current) setSearching(false);
      }
    }, DEBOUNCE_MS);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [enabled, pageSize, query]);

  // Unmount safety: a settled in-flight request must not write back.
  useEffect(() => {
    return () => {
      generationRef.current += 1;
    };
  }, []);

  return { results, searching };
}
