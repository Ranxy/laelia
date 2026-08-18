import type { IdentityProvider } from "@/types/proto-es/v1/idp_service_pb";
import { IdentityProviderType } from "@/types/proto-es/v1/idp_service_pb";

const OAUTH_STATE_PREFIX = "laelia_oauth_state_";
const OAUTH_STATE_TTL = 10 * 60 * 1000; // 10 minutes

export interface OAuthState {
  token: string;
  idpName: string;
  redirect?: string;
  timestamp: number;
}

function generateSecureToken(): string {
  const array = new Uint8Array(32);
  crypto.getRandomValues(array);
  return btoa(String.fromCharCode(...array))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=/g, "");
}

function storeOAuthState(state: OAuthState): void {
  localStorage.setItem(`${OAUTH_STATE_PREFIX}${state.token}`, JSON.stringify(state));
}

export function retrieveOAuthState(token: string): OAuthState | null {
  const key = `${OAUTH_STATE_PREFIX}${token}`;
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return null;
    const state = JSON.parse(raw) as OAuthState;
    if (Date.now() - state.timestamp > OAUTH_STATE_TTL) {
      localStorage.removeItem(key);
      return null;
    }
    return state;
  } catch {
    return null;
  }
}

export function clearOAuthState(token: string): void {
  try {
    localStorage.removeItem(`${OAUTH_STATE_PREFIX}${token}`);
  } catch {
    // ignore
  }
}

function isValidHttpUrl(url: string): boolean {
  try {
    const u = new URL(url);
    return u.protocol === "http:" || u.protocol === "https:";
  } catch {
    return false;
  }
}

export function buildOAuthAuthorizeUrl(
  provider: IdentityProvider,
  stateToken: string,
  redirectUri: string
): string | null {
  if (provider.type === IdentityProviderType.OAUTH2) {
    if (provider.config?.config?.case !== "oauth2Config") return null;
    const cfg = provider.config.config.value;
    if (!isValidHttpUrl(cfg.authUrl)) return null;
    const url = new URL(cfg.authUrl);
    url.searchParams.set("client_id", cfg.clientId);
    url.searchParams.set("response_type", "code");
    url.searchParams.set("redirect_uri", redirectUri);
    url.searchParams.set("state", stateToken);
    if (cfg.scopes?.length) {
      url.searchParams.set("scope", cfg.scopes.join(" "));
    }
    return url.toString();
  }
  // Only OAuth2 is supported for now.
  return null;
}

export function startOAuthLogin(
  provider: IdentityProvider,
  redirect?: string
): boolean {
  const token = generateSecureToken();
  storeOAuthState({
    token,
    idpName: provider.name,
    redirect,
    timestamp: Date.now(),
  });
  const redirectUri = `${window.location.origin}/oauth/callback`;
  const url = buildOAuthAuthorizeUrl(provider, token, redirectUri);
  if (!url) return false;
  window.location.assign(url);
  return true;
}
