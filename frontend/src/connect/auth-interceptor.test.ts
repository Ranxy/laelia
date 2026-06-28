import { Code, ConnectError, type Interceptor } from "@connectrpc/connect";
import { describe, expect, it, vi } from "vitest";
import { createAuthInterceptor } from "./auth-interceptor";

// The interceptor only inspects the response/error, never the request body, so
// a no-op request suffices. Mock `next` functions are cast to the interceptor's
// expected parameter type (their return values intentionally omit the full
// StreamResponse method/service fields — the interceptor never reads them).
type NextFn = Parameters<Interceptor>[0];
const req = {} as never;

async function drain<T>(message: AsyncIterable<T>): Promise<T[]> {
  const out: T[] = [];
  for await (const m of message) {
    out.push(m);
  }
  return out;
}

describe("createAuthInterceptor", () => {
  it("invokes the handler and rethrows on a unary Unauthenticated error", async () => {
    const handler = vi.fn();
    const err = new ConnectError("unauthenticated", Code.Unauthenticated);
    const next = (async () => {
      throw err;
    }) as unknown as NextFn;
    const call = createAuthInterceptor(handler)(next);

    await expect(call(req)).rejects.toBe(err);
    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledWith(err);
  });

  it("passes non-Unauthenticated errors through without invoking the handler", async () => {
    const handler = vi.fn();
    const err = new ConnectError("denied", Code.PermissionDenied);
    const next = (async () => {
      throw err;
    }) as unknown as NextFn;
    const call = createAuthInterceptor(handler)(next);

    await expect(call(req)).rejects.toBe(err);
    expect(handler).not.toHaveBeenCalled();
  });

  it("returns a successful unary response untouched", async () => {
    const handler = vi.fn();
    const res = {
      stream: false as const,
      message: {},
      header: new Headers(),
      trailer: new Headers(),
    };
    const next = (async () => res) as unknown as NextFn;
    const call = createAuthInterceptor(handler)(next);

    await expect(call(req)).resolves.toBe(res);
    expect(handler).not.toHaveBeenCalled();
  });

  it("handles Unauthenticated surfaced mid-stream", async () => {
    const handler = vi.fn();
    const err = new ConnectError("unauth", Code.Unauthenticated);
    async function* messages() {
      yield "a";
      throw err;
    }
    const streamRes = {
      stream: true as const,
      message: messages(),
      header: new Headers(),
      trailer: new Headers(),
    };
    const next = (async () => streamRes) as unknown as NextFn;
    const call = createAuthInterceptor(handler)(next);

    const res = await call(req);
    expect(res.stream).toBe(true);
    if (!res.stream) {
      throw new Error("expected a streaming response");
    }

    await expect(drain(res.message)).rejects.toBe(err);
    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledWith(err);
  });

  it("does not invoke the handler for a healthy stream", async () => {
    const handler = vi.fn();
    async function* messages() {
      yield "a";
      yield "b";
    }
    const streamRes = {
      stream: true as const,
      message: messages(),
      header: new Headers(),
      trailer: new Headers(),
    };
    const next = (async () => streamRes) as unknown as NextFn;
    const call = createAuthInterceptor(handler)(next);

    const res = await call(req);
    if (!res.stream) {
      throw new Error("expected a streaming response");
    }

    const collected = await drain(res.message);
    expect(collected).toEqual(["a", "b"]);
    expect(handler).not.toHaveBeenCalled();
  });
});
