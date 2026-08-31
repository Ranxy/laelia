import { create } from "@bufbuild/protobuf";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { iamServiceClient } from "@/connect";
import { showErrorToast } from "@/lib/toast-errors";
import {
  type IamPolicy,
  IamPolicySchema,
} from "@/types/proto-es/store/policy_pb";
import type { IamPolicyView } from "@/types/proto-es/v1/iam_service_pb";

// ---------------------------------------------------------------------------
// useIamPolicy — the settings-iam page's workspace-policy state.
//
// The IAM policy is not a flat CRUD list (useResourceQuery's shape), so the
// load/save/etag machinery stays a page-local hook, but it mirrors that
// hook's conventions: same query semantics (staleTime 0, retry false), the
// one-toast-per-failure-episode rule (notifiedRef), and a failure title
// passed in already-translated so this hook stays free of i18n imports.
//
// savePolicy is the one deliberate difference from useCrudDialog-style
// runners: it does NOT toast and does NOT swallow errors. A failure —
// typically the etag-mismatch ConnectError (Code.Aborted) written by the
// optimistic lock — throws to the sheet's catch block, keeping the sheet's
// own error Alert + reload semantics; a success is written back into the
// query cache (policy + fresh etag).
// ---------------------------------------------------------------------------

const POLICY_QUERY_KEY = ["settings", "workspaceIamPolicy"] as const;

// The policy resource together with the etag its next Set must carry.
export interface IamPolicyState {
  policy: IamPolicy;
  etag: string;
}

export interface UseIamPolicyOptions {
  // false keeps the query idle — the page gates on laelia.iam.getPolicy and
  // renders its PermissionNotice instead of fetching.
  enabled: boolean;
  // Already-translated failure title (t("settings.iam.load-failed") at the
  // call site; same contract as useResourceQuery's failureTitle).
  failureTitle: string;
}

export function useIamPolicy(opts: UseIamPolicyOptions): {
  policyState: IamPolicyState | null;
  // No cached data yet — render the page skeleton instead of the table.
  initialLoading: boolean;
  // A refetch is in flight while usable data is already on screen.
  refreshing: boolean;
  // Explicit re-fetch; the sheets await it inside the etag-mismatch catch
  // so the spinner covers the whole recovery, like the old load().
  reload: () => Promise<void>;
  // Throws on failure (etag conflict, permission…) — callers own the
  // presentation. Returns the server's response view.
  savePolicy: (edited: IamPolicy, etag: string) => Promise<IamPolicyView>;
} {
  const { enabled, failureTitle } = opts;
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: POLICY_QUERY_KEY,
    enabled,
    staleTime: 0,
    // Action semantics: one RPC attempt per (re)load — retries are a page
    // concern, and the failure toast must fire deterministically.
    retry: false,
    queryFn: async ({ signal }) => {
      const res = await iamServiceClient.getWorkspaceIamPolicy({}, { signal });
      return {
        policy: res.policy ?? create(IamPolicySchema, {}),
        etag: res.etag,
      };
    },
  });

  // One toast per failure episode: entering the error state toasts once;
  // follow-up refetches of the still-broken query stay quiet until a
  // result clears the error (useResourceQuery pattern).
  const notifiedRef = useRef(false);
  useEffect(() => {
    if (query.isError) {
      if (!notifiedRef.current) {
        notifiedRef.current = true;
        void showErrorToast(query.error, failureTitle);
      }
    } else {
      notifiedRef.current = false;
    }
  }, [query.isError, query.error, failureTitle]);

  return {
    policyState: query.data ?? null,
    initialLoading: query.isPending && enabled,
    refreshing: query.isFetching && !query.isPending,
    reload: async () => {
      await query.refetch();
    },
    savePolicy: async (edited, etag) => {
      const res = await iamServiceClient.setWorkspaceIamPolicy({
        policy: edited,
        etag,
      });
      queryClient.setQueryData<IamPolicyState>(POLICY_QUERY_KEY, {
        policy: res.policy ?? create(IamPolicySchema, {}),
        etag: res.etag,
      });
      return res;
    },
  };
}
