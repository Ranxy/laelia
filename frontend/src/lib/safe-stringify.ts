// lib/safe-stringify.ts
//
// JSON.stringify throws on BigInt, and protobuf int64 fields are BigInt in
// proto-es payloads — token/context counts and other int64s break raw
// `JSON.stringify`. Convert them to strings so payloads render.

export function safeStringify(value: unknown): string {
  return JSON.stringify(
    value,
    (_key, v) => (typeof v === "bigint" ? v.toString() : v),
    2
  );
}
