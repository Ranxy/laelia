// Laelia Web Push service worker.
//
// Receives encrypted push payloads from the manager (see
// backend/manager/component/webpush and store.buildPushPayload) and shows a
// system notification, unless the user is already viewing the conversation in
// a focused tab — in that case it postMessages PUSH_SUPPRESSED back to the page
// so an in-app toast can surface instead. The page tells the SW which route is
// currently open via SUPPRESS_ROUTE messages (more reliable than URL matching
// alone, since the chat can be open without the URL having changed).
//
// Payload shape (store.pushPayload):
//   { title, body, conversation, messageId, category, route }

// The route the page is currently viewing, or null. Pushes for this route are
// suppressed (the user is already looking at them).
let suppressedRoute = null;

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
    suppressedRoute = data.route || null;
  }
});

self.addEventListener("push", (event) => {
  let payload;
  try {
    payload = event.data ? event.data.json() : null;
  } catch {
    payload = null;
  }
  if (!payload || !payload.title) return;
  event.waitUntil(handlePush(payload));
});

async function handlePush(payload) {
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
async function isViewingRoute(route) {
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

async function broadcastSuppressed(payload) {
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

async function focusOrOpen(route) {
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