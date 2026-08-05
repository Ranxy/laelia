import { notificationServiceClient } from "@/connect";

// Web Push device state is derived entirely from the server: a browser is
// "subscribed" when its current PushSubscription endpoint appears in the
// user's registered-subscription list (ListPushSubscriptions). No localStorage
// intent flag is involved, so the settings toggle always reflects what the
// server will actually deliver to. The boot hook only refreshes an endpoint
// the server already knows (browsers rotate p256dh/auth keys), and never
// creates a row for an unknown endpoint — so a deliberately-disabled device is
// never silently re-enabled.

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

// listSubscriptions returns the current user's registered browser push
// endpoints (one per device/browser).
async function listSubscriptions(): Promise<string[]> {
  const res = await notificationServiceClient.listPushSubscriptions({});
  return res.pushSubscriptions.map((s) => s.endpoint);
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
// permission, subscribes, and registers the subscription server-side. Throws
// with a code the UI can branch on ("denied" | "not-configured" | other).
export async function enableDesktopNotifications(): Promise<void> {
  if (!webPushSupported()) throw new Error("unsupported");
  const perm = await Notification.requestPermission();
  if (perm !== "granted") throw new Error("denied");
  const reg = await getRegistration();
  await subscribe(reg);
}

// disable turns desktop notifications off: deletes the server subscription
// (so delivery stops immediately) and unsubscribes the browser. Both are
// best-effort and independent: a leftover server row is cleaned by the
// 404/410 path on the next push attempt, and a leftover browser subscription
// is never re-registered (the boot hook only refreshes endpoints the server
// already knows), so a failed unsubscribe cannot silently re-enable push.
export async function disableDesktopNotifications(): Promise<void> {
  if (!webPushSupported()) return;
  const reg = await getRegistration();
  const sub = await reg.pushManager.getSubscription();
  if (!sub) return;
  const endpoint = sub.endpoint;
  try {
    await notificationServiceClient.deletePushSubscription({
      name: subscriptionName(endpoint),
    });
  } catch {
    // ignore — stale row cleaned by the 404/410 path on the next push attempt.
  }
  try {
    await sub.unsubscribe();
  } catch {
    // ignore — the browser subscription is best-effort.
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

// isDeviceSubscribed reports whether the current browser is registered for
// push: its PushSubscription endpoint must appear in the user's server-side
// subscription list. This is the source of truth for the settings toggle.
export async function isDeviceSubscribed(): Promise<boolean> {
  if (!webPushSupported()) return false;
  const reg = await getRegistration();
  const browserSub = await reg.pushManager.getSubscription();
  if (!browserSub) return false;
  const endpoints = await listSubscriptions();
  return endpoints.includes(browserSub.endpoint);
}

// reconcilePushSubscription is called on app boot. It refreshes the server-side
// keys for this browser's subscription when its endpoint is already registered
// (browsers rotate p256dh/auth keys, which would otherwise break encryption),
// and leaves unknown endpoints alone so a disabled device is never resurrected.
export async function reconcilePushSubscription(): Promise<void> {
  if (!webPushSupported()) return;
  if (Notification.permission !== "granted") return;
  const reg = await getRegistration();
  const browserSub = await reg.pushManager.getSubscription();
  if (!browserSub) return;
  const endpoints = await listSubscriptions();
  if (endpoints.includes(browserSub.endpoint)) {
    try {
      await registerWithBackend(browserSub);
    } catch {
      // best-effort; the user can re-enable from settings.
    }
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
