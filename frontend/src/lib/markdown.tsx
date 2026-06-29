import { MarkdownCodeBlockNode, setCustomComponents } from "markstream-react";

// Centralizes the markstream-react custom component registry so both chat pages
// share the exact same registration. Calling setCustomComponents at module
// top-level mutates a global registry; importing this module from each page
// guarantees the registration runs exactly once and is never overwritten by a
// second import (which would happen if each page registered independently).
//
// The `registered` guard is a belt-and-suspenders against double-import (e.g.
// under Vite HMR or lazy route prefetch) so the registry is only touched on the
// very first evaluation of this module.
let registered = false;

function registerMarkdownComponents(): void {
  if (registered) return;
  registered = true;
  setCustomComponents({
    // biome-ignore lint/suspicious/noExplicitAny: markstream custom component API is loosely typed
    code_block: ({ node, isDark, ctx }: any) => (
      <MarkdownCodeBlockNode
        node={node}
        isDark={isDark}
        stream={ctx?.codeBlockStream}
        {...(ctx?.codeBlockProps ?? {})}
      />
    ),
  });
}

// Side-effect import: any module importing this file gets the registration
// applied exactly once. Markstream's registry is global per app instance, so a
// single registration covers every <MarkdownRender> consumer.
registerMarkdownComponents();

export {};
