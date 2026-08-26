/// <reference lib="webworker" />
import {
  cleanupOutdatedCaches,
  createHandlerBoundToURL,
  precacheAndRoute,
} from "workbox-precaching";
import { NavigationRoute, registerRoute } from "workbox-routing";
import { NetworkFirst } from "workbox-strategies";

declare const self: ServiceWorkerGlobalScope;

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

// Workbox injects the list of precached URLs here at build time and wires up
// cache-first serving for those static assets plus cleanup of outdated caches.
precacheAndRoute(self.__WB_MANIFEST);
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

// SPA navigation (deep links and the root route): prefer the network so new
// deploys are picked up, and when offline fall back to the PRECACHED app shell.
// Workbox stores the hashed /index.html in its own precache cache (keyed with a
// revision query), so we must not read a hand-rolled "laelia-app-shell" cache —
// that cache is never populated and always misses. createHandlerBoundToURL
// reads the actual precache entry. API / dynamic endpoints (/v1, /api, ...)
// are never matched by any Workbox route here, so they always go to the
// network untouched.
const navigationRoute = new NavigationRoute(
  new NetworkFirst({
    cacheName: "laelia-navigations",
    networkTimeoutSeconds: 10,
  })
);
navigationRoute.setCatchHandler(createHandlerBoundToURL("/index.html"));
registerRoute(navigationRoute);

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
