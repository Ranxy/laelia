import { fromBinary } from "@bufbuild/protobuf";
import { ConnectError } from "@connectrpc/connect";
import { PermissionDeniedDetailSchema } from "@/types/proto-es/v1/common_pb";

/**
 * permissionDeniedInfo extracts the structured PermissionDeniedDetail attached
 * by the backend IAM interceptor (missing permission + resources), or null when
 * the error carries none.
 */
export function permissionDeniedInfo(
  err: unknown
): { permissions: string[]; resources: string[] } | null {
  if (!(err instanceof ConnectError)) return null;
  for (const detail of err.details) {
    if (
      "type" in detail &&
      detail.type === "laelia.v1.PermissionDeniedDetail" &&
      "value" in detail
    ) {
      const decoded = fromBinary(
        PermissionDeniedDetailSchema,
        (detail as { value: Uint8Array }).value
      );
      return {
        permissions: decoded.requiredPermissions,
        resources: decoded.resources,
      };
    }
  }
  return null;
}

/**
 * describeError renders a stable human-readable error message, preferring the
 * structured permission-denied detail when present.
 */
export function describeError(err: unknown): string {
  const denied = permissionDeniedInfo(err);
  if (denied) {
    const parts: string[] = [];
    if (denied.permissions.length > 0) {
      parts.push(`missing ${denied.permissions.join(", ")}`);
    }
    if (denied.resources.length > 0) {
      parts.push(`on ${denied.resources.join(", ")}`);
    }
    if (parts.length > 0) return parts.join(" ");
  }
  return err instanceof Error ? err.message : String(err);
}
