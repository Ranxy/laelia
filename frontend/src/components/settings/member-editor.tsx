import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { MemberPicker } from "@/components/member-picker";
import { FieldRow } from "@/components/ui/field-row";
import { Badge } from "@/components/ui/badge";
import { memberLabel } from "@/lib/members";
import type { Group } from "@/types/proto-es/v1/group_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

// ---------------------------------------------------------------------------
// MemberEditor — the member badge list + MemberPicker row editor shared by
// the mcp-servers / api-providers forms (the two byte-identical blocks,
// 01-R9). Members are workspace references ("users/<id>", "groups/<email>",
// "allUsers"); rendered via the shared memberLabel. Labels are passed in
// pre-translated (the i18n scan needs the t() at the page call site).
// ---------------------------------------------------------------------------

interface MemberEditorProps {
  members: string[];
  users: User[];
  groups: Group[];
  onChange: (members: string[]) => void;
  label: string;
  hint?: string;
}

export function MemberEditor(props: MemberEditorProps) {
  const { members, users, groups, onChange, label, hint } = props;
  const { t } = useTranslation();
  const usedMembers = useMemo(
    () => new Set(members.filter(Boolean)),
    [members]
  );

  return (
    <div className="flex flex-col gap-2">
      <FieldRow label={label} hint={hint}>
        <div className="flex flex-col gap-2">
          {members.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {members.map((m) => (
                <Badge key={m} variant="secondary" className="gap-1.5">
                  {memberLabel(m, users, groups)}
                  <button
                    type="button"
                    className="text-control-placeholder hover:text-error"
                    onClick={() => onChange(members.filter((x) => x !== m))}
                    aria-label={t("common.remove")}
                  >
                    ×
                  </button>
                </Badge>
              ))}
            </div>
          )}
          <MemberPicker
            users={users}
            groups={groups}
            value=""
            allowAllUsers
            onSelect={(member) => {
              if (!member || usedMembers.has(member)) return;
              onChange([...members, member]);
            }}
          />
        </div>
      </FieldRow>
    </div>
  );
}