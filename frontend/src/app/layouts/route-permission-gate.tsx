import { Lock } from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useMatches, useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import type { RouteHandle } from "@/router/route-info";
import { useAppStore } from "@/stores";

// Route-level permission gate (audit 06 Rt-02). Dashboard routes may declare
// `handle.permission` — the same permission(s) the sidebar and settings menu
// use to hide the entry — and this gate refuses to render the matched page
// when the signed-in caller holds none of them, instead of letting the deep
// link reach the page and fail on its APIs.
//
// A component gate (not a route loader) on purpose: loaders only run on
// navigations, so a hard refresh on a protected deep link would evaluate the
// permission set before the session arrives and could never re-check. The
// gate renders reactively, so the decision is re-evaluated the moment
// currentUser (and its permission set) lands in the store.

// permissionGroupsFor flattens the matched handles into a list of required
// permission groups. Each group is ANY-of (holding one permission in the
// group satisfies it, mirroring the sidebar's `a || b` view gates); the
// caller must satisfy every group that declares one.
export function permissionGroupsFor(handles: unknown[]): string[][] {
  const groups: string[][] = [];
  for (const handle of handles) {
    const permission = (handle as RouteHandle | undefined)?.permission;
    if (!permission) continue;
    groups.push(Array.isArray(permission) ? permission : [permission]);
  }
  return groups;
}

// deniedByPermissions reports whether any group is fully ungranted.
export function deniedByPermissions(
  groups: string[][],
  granted: readonly string[] | undefined
): boolean {
  return groups.some(
    (group) => !group.some((permission) => granted?.includes(permission))
  );
}

function PermissionDenied() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  return (
    <div
      role="alert"
      className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center"
    >
      <Lock className="size-5 text-control-light" />
      <p className="text-sm font-medium text-main">
        {t("router.forbidden-title")}
      </p>
      <p className="max-w-sm text-sm text-control-light">
        {t("router.forbidden-description")}
      </p>
      <Button variant="outline" size="sm" onClick={() => navigate("/")}>
        {t("router.forbidden-back")}
      </Button>
    </div>
  );
}

export function RoutePermissionGate() {
  const matches = useMatches();
  const granted = useAppStore((s) => s.currentUser?.permissions);
  const denied = useMemo(
    () =>
      deniedByPermissions(
        permissionGroupsFor(matches.map((match) => match.handle)),
        granted
      ),
    [matches, granted]
  );
  return denied ? <PermissionDenied /> : <Outlet />;
}
