import { useQuery } from "@tanstack/react-query";
import { settingServiceClient } from "@/connect";

// ---------------------------------------------------------------------------
// useWorkspacePolicy — the public GetWorkspaceInfo read (09 章 A-5 收敛).
//
// One shared ["workspace-policy"] cache entry replaces the three hand-rolled
// mount effects that each fired the same unauthenticated RPC (sign-in gates
// the signup link, sign-up gates the form and reads verification/domain
// policy, the machines list gates user machine creation). The app-default
// Query semantics give the signin → signup hop a cache hit instead of a
// duplicate RPC.
//
// Every consumer treats "no data yet" as the permissive default and keeps the
// UI usable: the backend still enforces the policy server-side, so a failed
// read must not block the flow (previous pages silently swallowed errors —
// preserved here by rendering the defaults until/unless data arrives).
// ---------------------------------------------------------------------------

export interface WorkspacePolicy {
  signupDisallowed: boolean;
  requireEmailVerification: boolean;
  enforceIdentityDomain: boolean;
  // Populated only when the workspace enforces identity domains; otherwise an
  // empty list so callers can render the hint purely from `length`.
  allowedDomains: string[];
  userCreateMachineDisallowed: boolean;
}

const DEFAULT_POLICY: WorkspacePolicy = {
  signupDisallowed: false,
  requireEmailVerification: false,
  enforceIdentityDomain: false,
  allowedDomains: [],
  userCreateMachineDisallowed: false,
};

export function useWorkspacePolicy(): WorkspacePolicy {
  const query = useQuery({
    queryKey: ["workspace-policy"],
    queryFn: () => settingServiceClient.getWorkspaceInfo({}),
    // Public workspace config changes at admin pace; a longer window keeps
    // the auth-page hop cache-hit. refetchOnMount refresher is unnecessary.
    staleTime: 5 * 60_000,
    select: (res): WorkspacePolicy => ({
      signupDisallowed: res.disallowSignup,
      requireEmailVerification: res.requireEmailVerification ?? false,
      enforceIdentityDomain: res.enforceIdentityDomain,
      allowedDomains: res.enforceIdentityDomain ? (res.domains ?? []) : [],
      userCreateMachineDisallowed: res.disallowUserCreateMachine,
    }),
  });

  // Undefined data (in flight or failed) renders the permissive defaults.
  return query.data ?? DEFAULT_POLICY;
}
