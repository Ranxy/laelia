import { Code, ConnectError } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import { Loader2, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { iamServiceClient } from "@/connect";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import {
  BindingSchema,
  type Binding,
  IamPolicySchema,
  type IamPolicy,
} from "@/types/proto-es/store/policy_pb";

// AGENT_EDITOR_ROLE is the resource-id of the per-agent editor role. The agent
// policy only ever holds a single binding for this role; its members are the
// users (users/{uid}) who may edit the agent.
const AGENT_EDITOR_ROLE = "roles/agentEditor";

interface PolicyState {
  policy: IamPolicy;
  etag: string;
}

// AgentEditorsCard manages the per-agent IAM policy's agentEditor binding. The
// creator (agentEditor) and workspace admins resolve agents.edit server-side;
// this card is part of "editing the agent", so it is gated on the agent's
// canEdit flag rather than a workspace-scope permission.
export function AgentEditorsCard({
  agentName,
  canEdit,
}: {
  agentName: string;
  canEdit: boolean;
}) {
  const { t } = useTranslation();
  const users = useAppStore((s) => s.users);
  const fetchUsers = useAppStore((s) => s.fetchUsers);

  const [policyState, setPolicyState] = useState<PolicyState | null>(null);
  const [loading, setLoading] = useState(true);
  const [selectedUserName, setSelectedUserName] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await iamServiceClient.getAgentIamPolicy({ name: agentName });
      setPolicyState({
        policy: res.policy ?? create(IamPolicySchema, {}),
        etag: res.etag,
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [agentName]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    fetchUsers({ pageSize: 100 });
  }, [fetchUsers]);

  const emailByMember = useMemo(() => {
    const m = new Map<string, string>();
    for (const u of users) m.set(u.name, u.email);
    return m;
  }, [users]);

  // editorBinding is the agentEditor binding, or a fresh empty one if none yet.
  const editorBinding: Binding = useMemo(() => {
    const existing = policyState?.policy.bindings.find(
      (b) => b.role === AGENT_EDITOR_ROLE
    );
    return existing ?? create(BindingSchema, { role: AGENT_EDITOR_ROLE });
  }, [policyState]);

  // candidateUsers are users not already editors, for the add control.
  const candidateUsers = useMemo(() => {
    const editors = new Set(editorBinding.members);
    return users.filter((u) => !editors.has(u.name));
  }, [users, editorBinding]);

  async function savePolicy(next: Binding) {
    if (!policyState) return;
    setSaving(true);
    setError("");
    try {
      const bindings: Binding[] = [];
      for (const b of policyState.policy.bindings) {
        if (b.role === AGENT_EDITOR_ROLE) continue;
        bindings.push(b);
      }
      if (next.members.length > 0) bindings.push(next);
      const res = await iamServiceClient.setAgentIamPolicy({
        name: agentName,
        policy: { bindings },
        etag: policyState.etag,
      });
      setPolicyState({
        policy: res.policy ?? create(IamPolicySchema, {}),
        etag: res.etag,
      });
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Aborted) {
        setError(t("settings.iam.etag-mismatch"));
        await load();
      } else {
        setError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setSaving(false);
    }
  }

  async function handleAdd() {
    if (!selectedUserName) return;
    const members = [...editorBinding.members, selectedUserName];
    await savePolicy(create(BindingSchema, { role: AGENT_EDITOR_ROLE, members }));
    setSelectedUserName("");
    toastManager.add({ type: "success", title: t("agent.editors.added") });
  }

  async function handleRemove(member: string) {
    const members = editorBinding.members.filter((m) => m !== member);
    await savePolicy(create(BindingSchema, { role: AGENT_EDITOR_ROLE, members }));
    toastManager.add({ type: "success", title: t("agent.editors.removed") });
  }

  return (
    <section className="flex flex-col rounded-lg border border-control-border bg-background shadow-xs">
      <header className="border-b border-control-border px-5 py-3">
        <h2 className="text-sm font-semibold text-control">
          {t("agent.editors.title")}
        </h2>
      </header>
      <div className="flex flex-col gap-3 p-5">
        {error && <Alert variant="error" description={error} />}
        {!canEdit ? (
          <p className="text-xs text-control-light">
            {t("agent.profile.edit-not-allowed")}
          </p>
        ) : loading ? (
          <div className="flex items-center gap-2 text-sm text-control-light">
            <Loader2 className="size-4 animate-spin" />
            {t("common.loading")}
          </div>
        ) : (
          <>
            <div className="flex flex-col gap-1.5">
              {editorBinding.members.length === 0 ? (
                <p className="text-xs text-control-light">
                  {t("agent.editors.empty")}
                </p>
              ) : (
                editorBinding.members.map((member) => (
                  <div
                    key={member}
                    className="flex items-center justify-between gap-2"
                  >
                    <Badge variant="secondary">
                      {emailByMember.get(member) ?? member}
                    </Badge>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={saving}
                      onClick={() => handleRemove(member)}
                    >
                      <X className="size-3.5" />
                      {t("common.remove")}
                    </Button>
                  </div>
                ))
              )}
            </div>

            <div className="flex items-center gap-2">
              <Select
                value={selectedUserName}
                onValueChange={(v) => setSelectedUserName(String(v ?? ""))}
              >
                <SelectTrigger className="flex-1">
                  <SelectValue
                    placeholder={t("agent.editors.add-placeholder")}
                  />
                </SelectTrigger>
                <SelectContent>
                  {candidateUsers.map((u) => (
                    <SelectItem key={u.name} value={u.name}>
                      {u.email}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                disabled={saving || !selectedUserName}
                onClick={handleAdd}
              >
                {t("common.add")}
              </Button>
            </div>
          </>
        )}
      </div>
    </section>
  );
}
