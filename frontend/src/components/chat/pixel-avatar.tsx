import { useMemo } from "react";

// hash32 is a FNV-1a 32-bit hash, used to deterministically derive the
// identicon's shape and color from a stable seed (a user/agent id).
function hash32(str: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

// On-brand foreground hues (indigo family + a blue + a neutral). Picked from
// the CSS color vars so the identicon stays visually consistent with the rest
// of the UI and adapts if a dark theme is introduced.
const PALETTE = [
  "rgb(var(--color-accent))", // #4f46e5 indigo-600
  "rgb(var(--color-accent-hover))", // #3730a3 indigo-800
  "rgb(var(--color-accent-disabled))", // #a5b4fc indigo-300
  "rgb(var(--color-info))", // #2563eb blue-600
  "rgb(var(--color-control-light))", // #71717a gray-500
];

const GRID = 5;

// PixelAvatar renders a deterministic 5x5 mirrored identicon (GitHub-style)
// seeded by `seed`. No network, no storage: zero-bandwidth default avatar.
// The outer wrapper applies rounded-full so corners clip to a circle.
export function PixelAvatar({
  seed,
  size = 32,
}: {
  seed: string;
  size?: number;
}) {
  const { color, filled } = useMemo(() => {
    const h = hash32(seed || "0");
    const color = PALETTE[h % PALETTE.length];
    // Columns 0-2 carry the unique decisions (15 bits); columns 3-4 mirror
    // 1-0 so the identicon is left-right symmetric.
    const filled: boolean[] = [];
    for (let c = 0; c < 3; c++) {
      for (let r = 0; r < GRID; r++) {
        filled.push(((h >> (c * GRID + r)) & 1) === 1);
      }
    }
    return { color, filled };
  }, [seed]);

  const cell = size / GRID;
  const rects: React.ReactElement[] = [];
  for (let r = 0; r < GRID; r++) {
    for (let c = 0; c < GRID; c++) {
      const srcCol = c <= 2 ? c : GRID - 1 - c;
      if (!filled[srcCol * GRID + r]) continue;
      rects.push(
        <rect
          key={`${r}-${c}`}
          x={c * cell}
          y={r * cell}
          width={cell}
          height={cell}
          fill={color}
        />
      );
    }
  }

  return (
    <svg
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
      shapeRendering="crispEdges"
      aria-hidden="true"
    >
      <rect
        x={0}
        y={0}
        width={size}
        height={size}
        fill="rgb(var(--color-control-bg))"
      />
      {rects}
    </svg>
  );
}
