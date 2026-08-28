import { afterEach, describe, expect, it } from "vitest";
import { platformOwnsEdgeSwipe } from "./platform-edge-swipe";

// jsdom itself reports navigator.vendor === "Apple Computer, Inc." with no
// touch points (a WebKit-spec implementation), so every case stubs both.
function stubNavigator(overrides: Record<string, unknown>) {
  for (const [key, value] of Object.entries(overrides)) {
    Object.defineProperty(window.navigator, key, {
      value,
      configurable: true,
    });
  }
}

describe("platformOwnsEdgeSwipe", () => {
  afterEach(() => {
    for (const key of ["vendor", "maxTouchPoints", "standalone"] as const) {
      // Remove the own stub properties, restoring jsdom's prototype defaults.
      delete (window.navigator as unknown as Record<string, unknown>)[key];
    }
  });

  it("is false under Blink (desktop Chrome / devtools emulation)", () => {
    stubNavigator({ vendor: "Google Inc.", maxTouchPoints: 5 });
    expect(platformOwnsEdgeSwipe()).toBe(false);
  });

  it("is false for Android Chrome (touch, non-Apple vendor)", () => {
    stubNavigator({ vendor: "Google Inc.", maxTouchPoints: 5 });
    expect(platformOwnsEdgeSwipe()).toBe(false);
  });

  it("is false on desktop Mac Safari (no touch digitizer)", () => {
    stubNavigator({ vendor: "Apple Computer, Inc.", maxTouchPoints: 0 });
    expect(platformOwnsEdgeSwipe()).toBe(false);
  });

  it("is false when touch points are missing entirely (jsdom)", () => {
    stubNavigator({ vendor: "Apple Computer, Inc." });
    delete (window.navigator as unknown as Record<string, unknown>)
      .maxTouchPoints;
    expect(platformOwnsEdgeSwipe()).toBe(false);
  });

  it("is true on real iOS/iPadOS browsers (Apple vendor + multi-touch)", () => {
    stubNavigator({ vendor: "Apple Computer, Inc.", maxTouchPoints: 5 });
    expect(platformOwnsEdgeSwipe()).toBe(true);
  });

  it("is true inside a standalone home-screen PWA (modern iOS runs the same gesture)", () => {
    stubNavigator({
      vendor: "Apple Computer, Inc.",
      maxTouchPoints: 5,
      standalone: true,
    });
    Object.defineProperty(window, "matchMedia", {
      value: (query: string) => ({
        matches: query.includes("standalone"),
        media: query,
      }),
      configurable: true,
    });
    expect(platformOwnsEdgeSwipe()).toBe(true);
  });
});
