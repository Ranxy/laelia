import { Code, ConnectError, type Interceptor } from "@connectrpc/connect";

/**
 * Handler invoked when a request fails with `Code.Unauthenticated` — the signal
 * that the access cookie has expired mid-session. The default implementation
 * (wired in `connect/index.ts`) clears auth state and redirects to sign-in.
 */
export type UnauthenticatedHandler = (
  err: ConnectError
) => void | Promise<void>;

function isUnauthenticated(err: unknown): err is ConnectError {
  return err instanceof ConnectError && err.code === Code.Unauthenticated;
}

/**
 * Creates a transport interceptor that watches for `ConnectError` with code
 * `Unauthenticated`. On any such error it invokes `onUnauthenticated`, which
 * centralizes the "session expired" handling instead of relying on every call
 * site to catch 401s individually. Non-auth errors are passed through untouched.
 *
 * Both unary and streaming calls are covered: for streaming, an Unauthenticated
 * may surface while iterating the response stream rather than at call
 * establishment, so the response's message iterable is wrapped to catch
 * mid-stream auth failures too.
 */
export function createAuthInterceptor(
  onUnauthenticated: UnauthenticatedHandler
): Interceptor {
  return (next) => async (req) => {
    try {
      const res = await next(req);
      if (res.stream) {
        // For streaming calls, an Unauthenticated may surface while iterating the
        // response stream rather than at call establishment. Wrap the message
        // iterable so mid-stream auth failures are handled too. The spread keeps
        // every other field of the StreamResponse intact.
        const source = res.message;
        const wrapped: AsyncIterable<unknown> = {
          [Symbol.asyncIterator]() {
            return (async function* () {
              try {
                yield* source as AsyncIterable<unknown>;
              } catch (err) {
                if (isUnauthenticated(err)) {
                  await onUnauthenticated(err);
                }
                throw err;
              }
            })();
          },
        };
        return { ...res, message: wrapped } as typeof res;
      }
      return res;
    } catch (err) {
      if (isUnauthenticated(err)) {
        await onUnauthenticated(err);
      }
      throw err;
    }
  };
}
