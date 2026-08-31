import { useCallback, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { AcpConfigEditor } from "@/components/agent/acp-config-editor";
import { isPiProvider } from "@/components/profile-common";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { FieldRow } from "@/components/ui/field-row";
import { Input } from "@/components/ui/input";
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useAcpConfigDraft } from "@/composables/use-acp-config-draft";
import { describeError } from "@/lib/connect-errors";
import { useAppStore } from "@/stores";
import type { AgentACPConfigInput } from "@/stores/ui-models";
import type {
  AgentModelOption,
  AgentProviderInfo,
} from "@/types/proto-es/v1/agent_pb";

// ---------------------------------------------------------------------------
// MachineAddAgentSheet — the machine profile's create-agent drawer.
//
// Implements the AGENTS.md "outer wrapper + inner form + stable-entity-ref +
// key" pattern: the wrapper owns only `open` (plus the frozen sheet props /
// per-open remount key), and the inner form owns ALL form state, so the page's
// former ~20-field add-agent useState block and its resetAddForm() cascade
// disappear — the remount-per-open seeds a blank form every time (02-D6/02-P2).
// The ACP field blocks are the shared AcpConfigEditor in "create" mode, driven
// by the sheet-owned useAcpConfigDraft; the submit button and the create
// handler read the SAME canSubmit validity source.
// ---------------------------------------------------------------------------

interface MachineAddAgentSheetProps {
  open: boolean;
  // machines/{id} — the createAgent target and the model-probe scope.
  machineName: string;
  // Machine display title (description header). Frozen while open=false so
  // both header strings stay stable through the close animation.
  machineTitle: string;
  // machine.canCreateAgent — gates the editor's model-probe affordances like
  // the old inline form did.
  canCreateAgent: boolean;
  availableProviders: AgentProviderInfo[];
  // Workspace llm_agent_config.allow_user_self_provided_keys toggle.
  selfProvidedKeysEnabled: boolean;
  // onCreated fires after a successful createAgent: the page closes the sheet,
  // shows the "agent created" dialog and refetches machine + roster.
  onCreated: (title: string) => void;
  onClose: () => void;
}

export function MachineAddAgentSheet(props: MachineAddAgentSheetProps) {
  const { open, onClose } = props;
  const { t } = useTranslation();

  // Freeze the sheet props while open=false so the header stays visually
  // stable through the Sheet's close animation (Base UI's Portal unmounts
  // after the animation).
  const openPropsRef = useRef<MachineAddAgentSheetProps>(props);
  if (open) {
    openPropsRef.current = props;
  }
  const stableProps = openPropsRef.current;

  // One remount per open — the inner form's useState seeds fresh every time,
  // replacing the old page-level resetAddForm() cascade.
  const wasOpenRef = useRef(false);
  const openSeqRef = useRef(0);
  if (open !== wasOpenRef.current) {
    wasOpenRef.current = open;
    if (open) openSeqRef.current += 1;
  }

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <SheetContent width="wide">
        <SheetHeader>
          <SheetTitle>{t("machine.add-agent-title")}</SheetTitle>
          <SheetDescription>
            {t("machine.add-agent-description", {
              title: stableProps.machineTitle,
            })}
          </SheetDescription>
        </SheetHeader>
        <AddAgentForm
          key={openSeqRef.current}
          machineName={stableProps.machineName}
          canCreateAgent={stableProps.canCreateAgent}
          availableProviders={stableProps.availableProviders}
          selfProvidedKeysEnabled={stableProps.selfProvidedKeysEnabled}
          onCreated={stableProps.onCreated}
          onClose={onClose}
        />
      </SheetContent>
    </Sheet>
  );
}

interface AddAgentFormProps {
  machineName: string;
  canCreateAgent: boolean;
  availableProviders: AgentProviderInfo[];
  selfProvidedKeysEnabled: boolean;
  onCreated: (title: string) => void;
  onClose: () => void;
}

// Session-only model-refresh override, tagged with the provider it was probed
// for so a stale override is never applied after the provider changes (the old
// page reset refreshedModels in the provider-change cascade).
interface RefreshedModels {
  provider: string;
  models: AgentModelOption[];
}

function AddAgentForm({
  machineName,
  canCreateAgent,
  availableProviders,
  selfProvidedKeysEnabled,
  onCreated,
  onClose,
}: AddAgentFormProps) {
  const { t } = useTranslation();
  // Machine-form identity fields; the ACP config draft (provider, pi key
  // sources, custom command, env) moved into the shared AcpConfigEditor +
  // useAcpConfigDraft in create mode.
  const [agentName, setAgentName] = useState("");
  const [agentDescription, setAgentDescription] = useState("");
  const [personaPrompt, setPersonaPrompt] = useState("");
  const [allowAddToChannel, setAllowAddToChannel] = useState(false);
  const [adding, setAdding] = useState(false);
  const [addError, setAddError] = useState("");
  const [refreshedModels, setRefreshedModels] =
    useState<RefreshedModels | null>(null);

  // The create-mode ACP editor renders from THIS draft instance (the shared
  // edit page uses the editor's own hook instead).
  const draftCtl = useAcpConfigDraft();

  // Global API providers the caller may use, for the builtin-pi runtime's
  // managed provider/entry pickers (rendered inside the shared editor).
  const apiProviders = useAppStore((s) => s.apiProviders);

  // Mirror of the shared editor's model refresh: it probes the machine with
  // the current draft (including the unsaved persona, as before) and the
  // result is kept for the create-button validity, matching the old
  // page-level refreshedModels state. Failures bubble to the editor's
  // refresh-error UI; we invalidate any previous override (old behavior).
  const personaRef = useRef(personaPrompt);
  personaRef.current = personaPrompt;
  const handleRefreshModels = useCallback(
    async (input: AgentACPConfigInput) => {
      try {
        const models = await useAppStore.getState().refreshMachineModels(
          machineName,
          // The probe payload keeps the form's unsaved persona, like the old
          // page's refreshModels did.
          { ...input, personaPrompt: personaRef.current.trim() }
        );
        setRefreshedModels({ provider: input.provider, models });
        return models;
      } catch (err) {
        setRefreshedModels(null);
        throw err;
      }
    },
    [machineName]
  );
  // The editor fires onAutoSave where the old form cleared the error banner
  // on every field change; there is no save to orchestrate in create mode.
  const clearAddError = useCallback(() => setAddError(""), []);

  // ---- Validity: single source shared by the Create button and the handler
  // (02-D6: the old page had two handwritten copies of these checks). ACP
  // completeness comes from the shared draft's canSave; the create-flow
  // extras the machine form truly owns compose on top: the agent name, the
  // self-provided key/base url (at creation there is no stored key to fall
  // back on, unlike the edit page where an empty key means "keep existing"),
  // and the session-refresh override of the model requirement.
  const { draft, toInput, canSave } = draftCtl;
  const draftValid = canSave(availableProviders);
  const isPiRuntime = isPiProvider(draft.provider);
  const selectedProviderInfo = availableProviders.find(
    (p) => p.providerId === draft.provider
  );
  const refreshedOverride =
    refreshedModels?.provider === draft.provider
      ? refreshedModels.models
      : null;
  const modelOptions = refreshedOverride ?? selectedProviderInfo?.models ?? [];
  const modelRequired =
    !isPiRuntime &&
    !!selectedProviderInfo?.supportsModelConfigOption &&
    modelOptions.length > 0;
  const piSelfIncomplete =
    isPiRuntime &&
    draft.piMode === "self" &&
    (draft.apiKey.trim() === "" ||
      (draft.apiProvider === "custom" && draft.apiBaseUrl.trim() === ""));
  const canSubmit =
    agentName.trim() !== "" &&
    draftValid &&
    !(modelRequired && draft.model.trim() === "") &&
    !piSelfIncomplete;

  async function handleCreate() {
    if (adding || !canSubmit) return;
    setAddError("");
    const name = agentName.trim();
    setAdding(true);
    try {
      const createAgent = useAppStore.getState().createAgent;
      await createAgent(
        name,
        machineName,
        toInput(personaPrompt.trim()),
        undefined,
        allowAddToChannel,
        agentDescription.trim()
      );
      onCreated(name);
    } catch (err) {
      setAddError(describeError(err));
    } finally {
      setAdding(false);
    }
  }

  return (
    <>
      <SheetBody>
        {addError && (
          <Alert variant="error" description={addError} className="mb-2" />
        )}
        <div className="flex flex-col gap-4">
          <FieldRow
            label={t("machine.field-agent-name")}
            htmlFor="add-agent-name"
          >
            <Input
              id="add-agent-name"
              value={agentName}
              placeholder={t("machine.add-agent-name-placeholder")}
              onChange={(e) => {
                setAgentName(e.target.value);
                setAddError("");
              }}
            />
          </FieldRow>

          <div className="flex flex-col gap-1">
            <label className="text-sm font-medium">
              {t("agent.profile.description")}
            </label>
            <Textarea
              className="text-sm min-h-[80px]"
              placeholder={t("agent.profile.description-placeholder")}
              value={agentDescription}
              onChange={(e) => {
                setAgentDescription(e.target.value);
                setAddError("");
              }}
            />
          </div>

          <AcpConfigEditor
            mode="create"
            acpConfig={undefined}
            // Create mode reuses agentName as the model-probe target so the
            // editor's refresh action stays enabled.
            agentName={machineName}
            availableProviders={availableProviders}
            apiProviders={apiProviders}
            canEdit={canCreateAgent}
            canEditAdminOnly={false}
            canSelfProvide={selfProvidedKeysEnabled}
            machineResourceID=""
            noProvidersHint={
              availableProviders.length === 0
                ? t("machine.add-agent-no-providers")
                : undefined
            }
            saveStatus="idle"
            onAutoSave={clearAddError}
            onRefreshModels={handleRefreshModels}
            draftController={draftCtl}
          />

          <div className="flex flex-col gap-1">
            <label className="text-sm font-medium">
              {t("agent.acp-config-persona-prompt")}
            </label>
            <Textarea
              className="font-mono text-sm min-h-[120px]"
              placeholder={t("agent.acp-config-persona-prompt-placeholder")}
              value={personaPrompt}
              onChange={(e) => {
                setPersonaPrompt(e.target.value);
                setAddError("");
              }}
            />
          </div>

          <FieldRow
            label={t("agent.allow-add-to-channel")}
            hint={t("agent.allow-add-to-channel-hint")}
          >
            <Switch
              checked={allowAddToChannel}
              onCheckedChange={setAllowAddToChannel}
            />
          </FieldRow>
        </div>
      </SheetBody>
      <SheetFooter>
        <Button variant="outline" onClick={onClose} disabled={adding}>
          {t("common.cancel")}
        </Button>
        <Button
          disabled={adding || !canSubmit}
          onClick={() => void handleCreate()}
        >
          {adding ? t("common.creating") : t("common.create")}
        </Button>
      </SheetFooter>
    </>
  );
}
