self.addEventListener("install", () => self.skipWaiting())
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()))

// NovaPanel deliberately avoids an offline cache. Configuration and runtime
// status must always come from the active server version.
self.addEventListener("fetch", () => {})
