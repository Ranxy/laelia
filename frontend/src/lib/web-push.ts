import { notificationServiceClient } from "@/connect";

// localStorage key recording the user's intent to receive desktop notifications.
// The boot hook re-registers the service worker + re-subscribes when this is
// "true" and the browser still grants permission, so a page reload (or a
// browser-rotated subscription) silently re-establishes push.
export const PUSH_ENABLED_KEY = "laelia.push.enabled";

const SW_URL = "/sw.js";

export interface PushPayload {
  title: string;
  body: string;
  conversation: string;
  messageId: string;
  category: string;
  route: string;
}

// supported reports whether the current browser can do Web Push at all.
export function webPushSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    "serviceWorker" in navigator &&
    "PushManager" in window &&
    "Notification" in window
  );
}

// getStoredEnabled reads the user's persisted intent.
export function getStoredEnabled(): boolean {
  try {
    return localStorage.getItem(PUSH_ENABLED_KEY) === "true";
  } catch {
    return false;
  }
}

function setStoredEnabled(value: boolean) {
  try {
    if (value) localStorage.setItem(PUSH_ENABLED_KEY, "true");
    else localStorage.removeItem(PUSH_ENABLED_KEY);
  } catch {
    // ignore storage errors (private mode etc.)
  }
}

// urlBase64ToUint8Array converts a base64url VAPID public key into the
// Uint8Array the PushSubscription API expects. Standard Web Push boilerplate.
function urlBase64ToUint8Array(base64: string): Uint8Array {
  const padded = base64.padEnd(
    base64.length + ((4 - (base64.length % 4)) % 4),
    "="
  );
  const binary = atob(padded.replace(/-/g, "+").replace(/_/g, "/"));
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

// base64Url encodes an ArrayBuffer as unpadded url-safe base64 for the
// p256dh/auth keys sent to the backend.
function base64Url(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

async function getRegistration(): Promise<ServiceWorkerRegistration> {
  const reg = await navigator.serviceWorker.register(SW_URL);
  await navigator.serviceWorker.ready;
  return reg;
}

// subscribe creates (or reuses) a PushSubscription for the VAPID public key
// and registers it with the backend. Returns the subscription so the caller
// can unsubscribe it on disable. Throws on permission denial or backend error.
async function subscribe(
  reg: ServiceWorkerRegistration
): Promise<PushSubscription> {
  const existing = await reg.pushManager.getSubscription();
  if (existing) {
    await registerWithBackend(existing);
    return existing;
  }
  const config = await notificationServiceClient.getPushConfig({});
  if (!config.enabled || !config.vapidPublicKey) {
    throw new Error("web push not configured");
  }
  const sub = await reg.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(
      config.vapidPublicKey
    ) as BufferSource,
  });
  await registerWithBackend(sub);
  return sub;
}

async function registerWithBackend(sub: PushSubscription): Promise<void> {
  const p256dh = sub.getKey("p256dh");
  const auth = sub.getKey("auth");
  if (!p256dh || !auth) {
    throw new Error("push subscription missing encryption keys");
  }
  await notificationServiceClient.createPushSubscription({
    endpoint: sub.endpoint,
    p256dh: base64Url(p256dh),
    auth: base64Url(auth),
  });
}

// enable turns desktop notifications on: registers the service worker, asks
// permission, subscribes, and persists the intent. Throws with a code the UI
// can branch on ("denied" | "not-configured" | other).
export async function enableDesktopNotifications(): Promise<void> {
  if (!webPushSupported()) throw new Error("unsupported");
  const perm = await Notification.requestPermission();
  if (perm !== "granted") throw new Error("denied");
  const reg = await getRegistration();
  await subscribe(reg);
  setStoredEnabled(true);
}

// disable turns desktop notifications off: unsubscribes the browser and
// deletes the backend subscription. Best-effort on the backend delete so a
// network failure still clears the local intent.
export async function disableDesktopNotifications(): Promise<void> {
  setStoredEnabled(false);
  if (!webPushSupported()) return;
  try {
    const reg = await getRegistration();
    const sub = await reg.pushManager.getSubscription();
    if (sub) {
      const endpoint = sub.endpoint;
      await sub.unsubscribe();
      try {
        await notificationServiceClient.deletePushSubscription({
          name: subscriptionName(endpoint),
        });
      } catch {
        // already unsubscribed locally; a stale backend row will be cleaned up
        // by the 404/410 path on the next push attempt.
      }
    }
  } catch {
    // ignore — the user asked to disable, so surface nothing further.
  }
}

// subscriptionName builds "users/0/pushSubscriptions/{endpointKey}". The
// server scopes the delete to the authenticated caller regardless of the
// {user} segment (it is decorative — the caller is the only owner), so the
// client does not need to know its own principal id. Only the endpointKey
// (url-safe base64 of the endpoint) is used to identify the subscription.
export function subscriptionName(endpoint: string): string {
  return `users/0/pushSubscriptions/${endpointKey(endpoint)}`;
}

function endpointKey(endpoint: string): string {
  // URL-safe base64 without padding, mirroring the server's endpointKey.
  const binary = unescape(encodeURIComponent(endpoint));
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

// reSubscribeIfEnabled is called on app boot: if the user previously enabled
// notifications and permission is still granted, re-register the SW and
// re-subscribe idempotently (handles browser-rotated subscriptions). Silently
// clears the stored intent if permission was revoked.
export async function reSubscribeIfEnabled(): Promise<void> {
  if (!getStoredEnabled() || !webPushSupported()) return;
  if (Notification.permission !== "granted") {
    setStoredEnabled(false);
    return;
  }
  try {
    const reg = await getRegistration();
    await subscribe(reg);
  } catch {
    // best-effort; the user can re-enable from settings.
  }
}

// suppressRoute tells the service worker that the page is currently viewing
// the given conversation route, so a push for that conversation should not
// show a system notification (the user is already looking at it). The SW
// receives the message and skips showNotification, instead postMessage-ing
// PUSH_SUPPRESSED back to the page for an in-app toast.
export async function suppressRoute(route: string | null): Promise<void> {
  if (!webPushSupported()) return;
  try {
    const reg = await navigator.serviceWorker.ready;
    reg.active?.postMessage({ type: "SUPPRESS_ROUTE", route });
  } catch {
    // ignore
  }
}
