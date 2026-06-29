// Minimal toast manager compatible with Base UI's ToastManager interface.
// We don't import from @base-ui internals since the package's exports map
// prevents deep imports.

type Listener = (event: { action: string; options: unknown }) => void;

let listener: Listener | null = null;

export const toastManager = {
  " subscribe"(fn: Listener) {
    listener = fn;
    return () => {
      listener = null;
    };
  },
  add(options: {
    title?: unknown;
    description?: unknown;
    type?: string;
    timeout?: number;
    [key: string]: unknown;
  }) {
    const id = Math.random().toString(36).slice(2) + Date.now().toString(36);
    listener?.({ action: "add", options: { id, ...options } });
    return id;
  },
  close(_id?: string) {
    // Base UI handles toast lifecycle internally.
  },
} as {
  " subscribe"(fn: Listener): () => void;
  add(options: Record<string, unknown>): string;
  close(id?: string): void;
};
