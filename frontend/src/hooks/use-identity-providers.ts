import { useQuery } from "@tanstack/react-query";
import { identityProviderServiceClient } from "@/connect";
import type { IdentityProvider } from "@/types/proto-es/v1/idp_service_pb";

// ---------------------------------------------------------------------------
// useIdentityProviders — the public IdP list read (09 章 A-5 收敛).
//
// The sign-in page (SSO buttons) and the OAuth login deep link
// (/oauth/login/{providerId}) both need the same list; one shared
// ["identity-providers"] entry replaces their two independent mount fetches,
// so starting the SSO flow from the deep link right after visiting sign-in
// reuses the cache.
// ---------------------------------------------------------------------------

const EMPTY_PROVIDERS: IdentityProvider[] = [];

export interface IdentityProvidersResult {
  providers: IdentityProvider[];
  // True once the read settled (success or failure); the OAuth deep link
  // waits for this before declaring "provider not found".
  loaded: boolean;
  error: boolean;
}

export function useIdentityProviders(): IdentityProvidersResult {
  const query = useQuery({
    queryKey: ["identity-providers"],
    queryFn: () => identityProviderServiceClient.listIdentityProviders({}),
    // Configured IdPs change at admin pace; the app-default staleTime window
    // also dedupes the signin → oauth-login hop.
    staleTime: 5 * 60_000,
  });

  return {
    providers: query.data?.identityProviders ?? EMPTY_PROVIDERS,
    loaded: query.isSuccess || query.isError,
    error: query.isError,
  };
}
