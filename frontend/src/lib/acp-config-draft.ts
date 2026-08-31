// Shared helpers for the ACP runtime-config draft editors of the agent and
// machine profile pages. Previously each profile carried its own private copy
// of this logic (02 chapter D-组): the fold drifted into three inline variants
// and the BigInt-aware serialization existed only as a page-local function.

// Folds the key-value editor entries into a customEnv map, dropping entries
// with empty keys (empty-value entries are kept so a user can set FOO="").
export function foldCustomEnv(
  entries: { key: string; value: string }[]
): Record<string, string> {
  const customEnv: Record<string, string> = {};
  for (const entry of entries) {
    const key = entry.key.trim();
    if (!key) continue;
    customEnv[key] = entry.value;
  }
  return customEnv;
}

// Converts a non-negative token count from the number input into the proto
// int64 representation. Zero/negative means "unset"; fractional input is
// truncated so BigInt never receives a non-integer number.
export function toOptionalBigInt(value: number): bigint | undefined {
  if (!Number.isFinite(value) || value <= 0) return undefined;
  return BigInt(Math.trunc(value));
}

// Serializes a config payload for dirty comparison. The default JSON.stringify
// throws on BigInt (proto int64 fields are bigint), so convert those to
// strings first.
export function stringifyConfigForComparison(cfg: unknown): string {
  return JSON.stringify(cfg, (_key, value) =>
    typeof value === "bigint" ? value.toString() : value
  );
}
