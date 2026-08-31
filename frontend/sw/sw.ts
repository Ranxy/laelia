/// <reference lib="webworker" />
import {
  addRoute,
  cleanupOutdatedCaches,
  createHandlerBoundToURL,
  precache,
} from "workbox-precaching";
import { NavigationRoute, registerRoute } from "workbox-routing";
import { NetworkFirst } from "workbox-strategies";

declare const self: ServiceWorkerGlobalScope;

// The WebWorker lib's NotificationOptions lags the spec: `renotify` (resurface
// an existing tag instead of silently dropping it) is valid at showNotification
// time per the Notifications API.
declare global {
  interface NotificationOptions {
    renotify?: boolean | undefined;
  }
}

// Laelia Service Worker.
//
// Two responsibilities live in this single worker (a scope can only have one):
//   1. PWA app-shell precaching (Workbox): cache index.html + hashed assets at
//      install time so the UI shell opens instantly and degrades gracefully
//      offline. API/user data is NEVER cached (network-only below).
//   2. Web Push notifications: receive encrypted push payloads from the manager
//      (see backend/manager/component/webpush and store.buildPushPayload) and
//      show a system notification, unless the user is already viewing the
//      conversation in a focused tab — in that case postMessages
//      PUSH_SUPPRESSED back to the page so an in-app toast can surface instead.
//      The page tells the SW which route is currently open via SUPPRESS_ROUTE
//      messages (more reliable than URL matching alone, since the chat can be
//      open without the URL having changed).
//
// Push payload shape (store.pushPayload):
//   { title, body, conversation, messageId, category, route }

// Route-ordering is load-bearing here: Workbox matches routes in registration
// order (first match wins), and PrecacheRoute maps navigations to "/" onto the
// precached "/index.html" (its directoryIndex default). Letting it register
// first — what precacheAndRoute() does — would serve "/" cache-first from the
// precache, pinning every PWA launch to the installed SW's build and hiding
// deploys until a successful (all-or-nothing) SW install. So:
//   1. precache()   — fills the precache cache via install/activate listeners,
//                     no routes yet (createHandlerBoundToURL below resolves its
//                     cache key against this list at startup, so it must run
//                     before the catch handler is built);
//   2. navigation route — claims ALL navigations network-first (fresh deploys
//                     win immediately; the precached shell is only the offline
//                     fallback via the catch handler);
//   3. addRoute()   — the precache route, which then only serves hashed assets
//                     that the navigation route did not already match.
precache(self.__WB_MANIFEST);

const navigationRoute = new NavigationRoute(
  new NetworkFirst({
    cacheName: "laelia-navigations",
    networkTimeoutSeconds: 10,
  })
);
navigationRoute.setCatchHandler(createHandlerBoundToURL("/index.html"));
registerRoute(navigationRoute);

addRoute();
cleanupOutdatedCaches();

// The route the page is currently viewing, or null. Pushes for this route are
// suppressed (the user is already looking at them).
let suppressedRoute: string | null = null;

self.addEventListener("install", (event) => {
  // Activate immediately so the first registration controls the page and push
  // events fire without waiting for a navigation.
  event.waitUntil(self.skipWaiting());
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("message", (event) => {
  const data = event.data;
  if (!data || typeof data !== "object") return;
  if (data.type === "SUPPRESS_ROUTE") {
    suppressedRoute = (data.route as string) || null;
  }
});

self.addEventListener("push", (event) => {
  let payload: PushPayload | null;
  try {
    payload = event.data ? event.data.json() : null;
  } catch {
    payload = null;
  }
  if (!payload || !payload.title) return;
  event.waitUntil(handlePush(payload));
});

interface PushPayload {
  title: string;
  body: string;
  conversation: string;
  messageId: string;
  category: string;
  route: string;
}

async function handlePush(payload: PushPayload) {
  const route = payload.route || "";
  const tag = payload.conversation || route;

  // Suppress when the page is focused and viewing this conversation, OR when
  // the page has explicitly told us it is viewing this route. In either case
  // hand the payload to the page for an in-app toast instead of a system
  // notification.
  const focused = await isViewingRoute(route);
  if (focused) {
    await broadcastSuppressed(payload);
    return;
  }
  await self.registration.showNotification(payload.title, {
    body: payload.body || "",
    tag,
    data: { route, payload },
    renotify: true,
  });
}

// isViewingRoute reports whether some window client is focused AND either its
// URL path matches the route or the page has set suppressedRoute to it.
async function isViewingRoute(route: string): Promise<boolean> {
  if (!route) return false;
  if (suppressedRoute && suppressedRoute === route) return true;
  const clients = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
  for (const client of clients) {
    if (!client.focused) continue;
    const path = new URL(client.url, self.location.origin).pathname;
    if (path === route) return true;
  }
  return false;
}

async function broadcastSuppressed(payload: PushPayload) {
  const clients = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
  for (const client of clients) {
    if (client.focused) {
      client.postMessage({ type: "PUSH_SUPPRESSED", payload });
    }
  }
}

self.addEventListener("notificationclick", (event) => {
  const route = event.notification.data && event.notification.data.route;
  event.notification.close();
  event.waitUntil(focusOrOpen(route));
});

async function focusOrOpen(route: string | null | undefined) {
  if (!route) {
    const clients = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    for (const client of clients) {
      if (client.focused) return client.focus();
    }
    return self.clients.openWindow("/");
  }
  const clients = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
  for (const client of clients) {
    const path = new URL(client.url, self.location.origin).pathname;
    if (path === route) {
      client.focus();
      client.postMessage({ type: "NOTIFICATION_CLICK", route });
      return;
    }
  }
  return self.clients.openWindow(route);
}
