import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Avatar } from "@/components/chat/avatar";
import { ConnectionBadge } from "@/components/connection-badge";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { useAppStore } from "@/stores";
import type { MemberSummary } from "@/stores/types";

// MembersPage is the flat workspace directory: humans and agents in a single
// list, not grouped by machine. An agent row opens that agent's DM; a human row
// is display-only (user-to-user DMs are not created from this view).
export function MembersPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const members = useAppStore((s) => s.members);
  const loading = useAppStore((s) => s.membersLoading);
  const error = useAppStore((s) => s.membersError);
  const fetchMembers = useAppStore((s) => s.fetchMembers);

  useEffect(() => {
    void fetchMembers();
  }, [fetchMembers]);

  function openMember(member: MemberSummary) {
    if (member.kind === "agent") {
      const resourceId = member.name.replace(/^agents\//, "");
      navigate(`/agents/${resourceId}/chat`);
    }
  }

  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      <div className="flex items-center justify-between gap-2 border-b border-control-border px-4 py-3 shrink-0 lg:px-6">
        <h1 className="text-sm font-semibold text-main truncate">
          {t("members.title")}
        </h1>
      </div>
      <div className="flex-1 overflow-y-auto p-4">
        <div className="mx-auto w-full max-w-3xl">
          {loading ? (
            <p className="text-sm text-control-light">{t("common.loading")}</p>
          ) : error ? (
            <div className="flex flex-col gap-3">
              <Alert variant="error" description={t("members.load-failed")} />
              <Button variant="outline" onClick={() => void fetchMembers()}>
                {t("common.retry")}
              </Button>
            </div>
          ) : members.length === 0 ? (
            <p className="text-sm text-control-light">{t("members.no-data")}</p>
          ) : (
            <ul className="flex flex-col gap-1">
              {members.map((member) => {
                const resourceId = member.name.replace(/^(agents|users)\//, "");
                const isAgent = member.kind === "agent";
                return (
                  <li key={`${member.kind}/${member.name}`}>
                    <div
                      role={isAgent ? "button" : undefined}
                      tabIndex={isAgent ? 0 : undefined}
                      className={
                        isAgent
                          ? "flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 hover:bg-control-bg/60 transition-colors"
                          : "flex items-center gap-3 rounded-md px-3 py-2"
                      }
                      onClick={isAgent ? () => openMember(member) : undefined}
                      onKeyDown={
                        isAgent
                          ? (e) => {
                              if (e.key === "Enter" || e.key === " ") {
                                e.preventDefault();
                                openMember(member);
                              }
                            }
                          : undefined
                      }
                    >
                      <Avatar seed={resourceId || member.title} />
                      <div className="min-w-0 flex-1 flex flex-col gap-0.5">
                        <span className="truncate text-sm font-medium text-main">
                          {member.title}
                        </span>
                        {member.subtitle && (
                          <span className="truncate text-xs text-control-light">
                            {isAgent
                              ? t("members.agent-subtitle", {
                                  machine: member.subtitle.replace(
                                    /^machines\//,
                                    ""
                                  ),
                                })
                              : member.subtitle}
                          </span>
                        )}
                      </div>
                      {isAgent ? (
                        <ConnectionBadge state={member.connectionState} />
                      ) : (
                        <span className="text-xs text-control-light">
                          {t("members.kind-user")}
                        </span>
                      )}
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
