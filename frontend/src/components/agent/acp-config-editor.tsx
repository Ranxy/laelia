import { Check, Loader2 } from "lucide-react";
import { memo, useImperativeHandle, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { KeyValueEnvEditor } from "@/components/agent/key-value-env-editor";
import { StringListEditor } from "@/components/agent/string-list-editor";
import {
  Card,
  entryLabel,
  isPiProvider,
  modelLabel,
  piAPIProviderIds,
  providerDisplayName,
  providerLabel,
} from "@/components/profile-common";
import { Button } from "@/components/ui/button";
import { ModelCombobox } from "@/components/ui/combobox";
import { Input } from "@/components/ui/input";
import { SecretInput } from "@/components/ui/secret-input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  type AcpConfigDraft,
  draftFromPersisted,
  useAcpConfigDraft,
} from "@/hooks/use-acp-config-draft";
import { usePiModelOptions } from "@/hooks/use-pi-model-options";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import type { AgentACPConfigInput } from "@/stores/ui-models";
import type {
  AgentACPConfig,
  AgentModelOption,
  AgentProviderInfo,
} from "@/types/proto-es/v1/agent_pb";
import type { ApiProvider } from "@/types/proto-es/v1/api_provider_service_pb";

// Save-status banner state for the runtime-config card header.
export type AcpSaveStatus = "idle" | "saving" | "saved" | "error";

// AcpConfigEditorHandle lets the page own save orchestration without owning
// the draft state: getDraftRef reads the CURRENT draft synchronously from
// async save closures (the page's old configRef mirror), and the other
// members reuse the draft hook's serialization / validity / dirtiness logic.
export interface AcpConfigEditorHandle {
  getDraftRef(): { current: AcpConfigDraft };
  getDraft(): AcpConfigDraft;
  toInput(personaPrompt: string): AgentACPConfigInput;
  canSave(availableProviders: AgentProviderInfo[]): boolean;
  isDirtyAgainst(persisted: AgentACPConfig | undefined): boolean;
}

export interface AcpConfigEditorProps {
  // Seed snapshot from the persisted config. It is read once at mount; the
  // page re-seeds per agent via key remount instead of a seed effect.
  acpConfig: AgentACPConfig | undefined;
  // Agent resource name, for the machine-model refresh gating.
  agentName: string;
  // Machine-scoped providers (provider/model selectors + canSave).
  availableProviders: AgentProviderInfo[];
  // Global API providers the caller may use (builtin-pi managed pickers).
  apiProviders: ApiProvider[];
  // agent.canEdit: gates the fieldset, machine link and machine model refresh.
  canEdit: boolean;
  // canEditAdminOnly gates the admin-only save path and the no-providers hint.
  canEditAdminOnly: boolean;
  // Whether non-admin owners may self-provide an inline api key.
  canSelfProvide: boolean;
  // machines/{id} of the owning machine ("" when unbound).
  machineResourceID: string;
  // Save-status banner in the card header.
  saveStatus: AcpSaveStatus;
  // Page-owned save entry point (the page's saveConfig). The editor never
  // serializes saves itself: it fires onAutoSave exactly where the page used
  // to call saveConfig (cascade onValueChange + blur on commit inputs).
  onAutoSave: () => void;
  // Probes the agent's machine for the selected provider's models with the
  // given (possibly unsaved) config. The page wires this to the store.
  onRefreshModels: (input: AgentACPConfigInput) => Promise<AgentModelOption[]>;
  // "edit" (default) renders the runtime-config Card for the agent profile
  // page; "create" renders the bare field list for embedding in the machine
  // profile's add-agent sheet (no Card chrome / save-status header there).
  mode?: "edit" | "create";
  // create mode only: the embedding sheet owns the draft via useAcpConfigDraft
  // and passes the hook result, so the owner's canSubmit computation and this
  // editor render from one shared draft instance. Undefined in edit mode,
  // where the editor owns the draft itself.
  draftController?: ReturnType<typeof useAcpConfigDraft>;
  // create mode only: hint rendered under the provider select when the machine
  // probes zero providers (the create sheet still offers builtin-pi + custom).
  noProvidersHint?: string;
}

function AcpConfigEditorImpl({
  acpConfig,
  agentName,
  availableProviders,
  apiProviders,
  canEdit,
  canEditAdminOnly,
  canSelfProvide,
  machineResourceID,
  saveStatus,
  onAutoSave,
  onRefreshModels,
  mode,
  draftController,
  noProvidersHint,
  ref,
}: AcpConfigEditorProps & { ref?: React.Ref<AcpConfigEditorHandle> }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const showLegacyInline = canEditAdminOnly || canSelfProvide;
  const isCreate = mode === "create";
  // The runtime-config draft lives here while in edit mode (remounted per
  // agent by the page), with a synchronous ref mirror so async save closures
  // always read the current values. In create mode the embedding sheet's
  // controller is used instead so both see one shared draft.
  const ownDraft = useAcpConfigDraft(draftFromPersisted(acpConfig));
  const {
    draft,
    draftRef,
    setField,
    setDraft,
    toInput,
    canSave,
    isDirtyAgainst,
  } = draftController ?? ownDraft;
  useImperativeHandle(
    ref,
    () => ({
      getDraftRef: () => draftRef,
      getDraft: () => draftRef.current,
      toInput,
      canSave,
      isDirtyAgainst,
    }),
    [draftRef, toInput, canSave, isDirtyAgainst]
  );

  // Machine-model refresh override: probes the selected provider's models on
  // the agent's machine with the current draft custom_env (e.g. CODEX_HOME),
  // so the picker reflects the env the user is configuring before saving.
  // Session-only; resets on provider change and on agent remount.
  const [refreshedModels, setRefreshedModels] = useState<
    AgentModelOption[] | null
  >(null);
  const [modelsRefreshing, setModelsRefreshing] = useState(false);
  const [modelsRefreshError, setModelsRefreshError] = useState("");
  // Dynamic model list for the builtin-pi self-provided mode, fetched from
  // the provider's model API via the manager and cached per api_provider.
  const piModels = usePiModelOptions();

  const isCustomProvider = draft.provider === "custom";
  const isPiRuntime = isPiProvider(draft.provider);
  // Global-provider selection for the pi runtime: the provider (one the caller
  // may use) and the entry (one (key, model) pair) the agent will use. The
  // model resolves from the entry server-side.
  const selectedGlobalProvider = apiProviders.find(
    (p) => p.name === draft.globalProvider
  );
  const globalProviderEntries = selectedGlobalProvider?.entries ?? [];
  const selectedProviderInfo = availableProviders.find(
    (p) => p.providerId === draft.provider
  );
  // A refresh (probe with the agent's custom env) overrides the
  // machine-discovered model list for this session. It resets when the
  // provider changes so a stale override is never shown for another provider.
  const modelOptions = refreshedModels ?? selectedProviderInfo?.models ?? [];
  const providerSupportsModel =
    !isPiRuntime && !!selectedProviderInfo?.supportsModelConfigOption;

  async function refreshModels() {
    if (!draft.provider || !agentName) return;
    setModelsRefreshing(true);
    setModelsRefreshError("");
    try {
      const models = await onRefreshModels(toInput(""));
      setRefreshedModels(models);
    } catch (err) {
      const msg = describeError(err);
      setModelsRefreshError(msg);
      setRefreshedModels(null);
      toastManager.add({
        type: "error",
        title: t("agent.acp-config-models-refresh-failed"),
        description: msg,
      });
    } finally {
      setModelsRefreshing(false);
    }
  }

  // Switching provider resets model + pi fields — the previous values belong
  // to the old runtime. protocol is custom-only; reset when leaving custom.
  function handleProviderChange(v: string | null) {
    const next = String(v ?? "");
    setDraft((d) => ({
      ...d,
      provider: next,
      model: "",
      protocol: next === "custom" ? d.protocol : "",
      piMode: next === "pi" ? "own" : "global",
      apiProvider: "",
      apiKey: "",
      globalProvider: "",
      globalProviderEntry: "",
      apiBaseUrl: "",
      contextWindow: 0,
      maxTokens: 0,
    }));
    setRefreshedModels(null);
    setModelsRefreshError("");
    onAutoSave();
  }

  // Switching pi key-source mode clears the abandoned side of the form and
  // the (provider-scoped) cached model list, then persists the now-incomplete
  // config only when it is still saveable (page gating).
  function handlePiModeChange(v: string | null) {
    const next = v === "own" || v === "self" ? v : "global";
    if (next === "global") {
      // Switching to managed: clear the inline/own side.
      setDraft((d) => ({
        ...d,
        apiProvider: "",
        apiKey: "",
        apiBaseUrl: "",
        model: "",
        contextWindow: 0,
        maxTokens: 0,
      }));
      piModels.clear();
    } else {
      // Switching to self/own: clear the managed side.
      setDraft((d) => ({
        ...d,
        globalProvider: "",
        globalProviderEntry: "",
      }));
    }
    if (next === "own") {
      setDraft((d) => ({
        ...d,
        apiProvider: "",
        apiKey: "",
        apiBaseUrl: "",
        model: "",
      }));
      piModels.clear();
    }
    setField("piMode", next);
    onAutoSave();
  }

  // Optional context-window/max-token inputs for a custom pi provider. Shared
  // by the self-provided and managed (global custom provider) modes.
  const renderPiContextFields = () => (
    <>
      <div className="flex flex-col gap-1">
        <label className="text-sm font-medium">
          {t("agent.acp-config-pi-context-window")}
        </label>
        <Input
          type="number"
          min={0}
          step={1}
          value={draft.contextWindow || ""}
          onChange={(e) => {
            const next = Number(e.target.value);
            const value =
              Number.isFinite(next) && next > 0 ? Math.trunc(next) : 0;
            setField("contextWindow", value);
          }}
          onBlur={() => onAutoSave()}
          placeholder={t("agent.acp-config-pi-context-window-placeholder")}
        />
        <p className="text-xs text-control-light">
          {t("agent.acp-config-pi-context-window-hint")}
        </p>
      </div>
      <div className="flex flex-col gap-1">
        <label className="text-sm font-medium">
          {t("agent.acp-config-pi-max-tokens")}
        </label>
        <Input
          type="number"
          min={0}
          step={1}
          value={draft.maxTokens || ""}
          onChange={(e) => {
            const next = Number(e.target.value);
            const value =
              Number.isFinite(next) && next > 0 ? Math.trunc(next) : 0;
            setField("maxTokens", value);
          }}
          onBlur={() => onAutoSave()}
          placeholder={t("agent.acp-config-pi-max-tokens-placeholder")}
        />
        <p className="text-xs text-control-light">
          {t("agent.acp-config-pi-max-tokens-hint")}
        </p>
      </div>
    </>
  );

  // The save-status header node only applies to the edit-mode Card; create
  // mode renders the bare field layout.
  const saveStatusActions =
    saveStatus === "saving" ? (
      <span className="flex items-center gap-1 text-xs text-control-light">
        <Loader2 className="size-3 animate-spin" />
        {t("agent.acp-config-saving")}
      </span>
    ) : saveStatus === "saved" ? (
      <span className="flex items-center gap-1 text-xs text-control-light">
        <Check className="size-3" />
        {t("agent.acp-config-saved")}
      </span>
    ) : saveStatus === "error" ? (
      <span className="flex items-center gap-1 text-xs text-error">
        <span className="size-1.5 rounded-full bg-error" />
        {t("agent.acp-config-save-error")}
      </span>
    ) : null;

  const body = (
    <fieldset disabled={!canEditAdminOnly && !canEdit} className="contents">
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <label className="text-sm font-medium">
            {t("agent.acp-config-provider")}
          </label>
          {!isCreate && availableProviders.length === 0 && !canEditAdminOnly ? (
            <p className="text-xs text-control-light">
              {machineResourceID
                ? t("agent.acp-config-no-providers-machine")
                : t("agent.acp-config-no-providers")}
            </p>
          ) : (
            <Select value={draft.provider} onValueChange={handleProviderChange}>
              <SelectTrigger>
                <SelectValue>
                  {(v: string | null) =>
                    v
                      ? v === "builtin-pi"
                        ? t("agent.acp-config-provider-builtin-pi")
                        : providerLabel(v, availableProviders)
                      : ""
                  }
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {/* builtin-pi is always available — it is bundled with
                    laelia, not host-detected — so it shows on every agent
                    regardless of the machine's probe results. */}
                <SelectItem value="builtin-pi">
                  {t("agent.acp-config-provider-builtin-pi")}
                </SelectItem>
                {availableProviders.map((p) => (
                  <SelectItem
                    key={p.providerId}
                    value={p.providerId}
                    disabled={p.compatible === false}
                  >
                    {providerDisplayName(p)}
                    {p.compatible === false && p.incompatibilityReason
                      ? ` — ${p.incompatibilityReason}`
                      : ""}
                  </SelectItem>
                ))}
                <SelectItem value="custom">
                  {t("agent.acp-config-provider-custom")}
                </SelectItem>
              </SelectContent>
            </Select>
          )}
          {isCreate && noProvidersHint && availableProviders.length === 0 && (
            <p className="text-xs text-control-light">{noProvidersHint}</p>
          )}
          {machineResourceID && canEdit && (
            <p className="text-xs text-control-light">
              <button
                type="button"
                className="text-link hover:underline"
                onClick={() => navigate(`/machines/${machineResourceID}`)}
              >
                {t("agent.acp-config-manage-providers")}
              </button>
            </p>
          )}
        </div>

        {isPiRuntime && (
          <>
            {(showLegacyInline || draft.provider === "pi") && (
              <div className="flex flex-col gap-1">
                <label className="text-sm font-medium">
                  {t("agent.acp-config-pi-mode")}
                </label>
                <Select value={draft.piMode} onValueChange={handlePiModeChange}>
                  <SelectTrigger>
                    <SelectValue>
                      {(v: string | null) =>
                        v === "own"
                          ? t("agent.acp-config-pi-mode-own")
                          : v === "self"
                            ? t("agent.acp-config-pi-mode-self")
                            : t("agent.acp-config-pi-mode-managed")
                      }
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {draft.provider === "pi" && (
                      <SelectItem value="own">
                        {t("agent.acp-config-pi-mode-own")}
                      </SelectItem>
                    )}
                    <SelectItem value="global">
                      {t("agent.acp-config-pi-mode-managed")}
                    </SelectItem>
                    {showLegacyInline && (
                      <SelectItem value="self">
                        {t("agent.acp-config-pi-mode-self")}
                      </SelectItem>
                    )}
                  </SelectContent>
                </Select>
              </div>
            )}

            {draft.piMode === "own" && draft.provider === "pi" && (
              <div className="flex flex-col gap-1">
                <label className="text-sm font-medium">
                  {t("agent.acp-config-model")}
                </label>
                {selectedProviderInfo?.models?.length ? (
                  <Select
                    value={draft.model}
                    onValueChange={(v) => {
                      const next = String(v ?? "");
                      setField("model", next);
                      onAutoSave();
                    }}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue>
                        {(v: string | null) =>
                          v
                            ? modelLabel(v, selectedProviderInfo?.models ?? [])
                            : ""
                        }
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {(selectedProviderInfo?.models ?? []).map((m) => (
                        <SelectItem key={m.value} value={m.value}>
                          {m.name || m.value}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : (
                  <p className="text-xs text-control-light">
                    {t("agent.acp-config-pi-own-models-empty")}
                  </p>
                )}
                <p className="text-xs text-control-light">
                  {t("agent.acp-config-pi-own-model-hint")}
                </p>
              </div>
            )}

            {draft.piMode === "global" && (
              <>
                <div className="flex flex-col gap-1">
                  <label className="text-sm font-medium">
                    {t("agent.acp-config-pi-global-provider")}
                  </label>
                  <Select
                    value={draft.globalProvider}
                    onValueChange={(v) => {
                      const next = String(v ?? "");
                      const nextProvider = apiProviders.find(
                        (p) => p.name === next
                      );
                      // Keep the optional context config only when the
                      // newly selected managed provider is a custom one
                      // (a built-in type fixes both by itself).
                      const keepContext =
                        nextProvider?.providerType === "custom";
                      setDraft((d) => ({
                        ...d,
                        globalProvider: next,
                        globalProviderEntry: "",
                        contextWindow: keepContext ? d.contextWindow : 0,
                        maxTokens: keepContext ? d.maxTokens : 0,
                      }));
                      onAutoSave();
                    }}
                  >
                    <SelectTrigger>
                      <SelectValue>
                        {(v: string | null) =>
                          v
                            ? (apiProviders.find((p) => p.name === v)?.title ??
                              v)
                            : ""
                        }
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {apiProviders.length === 0 && (
                        <SelectItem value="__no_provider" disabled>
                          {t("agent.acp-config-pi-global-providers-empty")}
                        </SelectItem>
                      )}
                      {apiProviders.map((p) => (
                        <SelectItem key={p.name} value={p.name}>
                          {p.title}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                {draft.globalProvider &&
                  (globalProviderEntries.length > 0 ? (
                    <div className="flex flex-col gap-1">
                      <label className="text-sm font-medium">
                        {t("agent.acp-config-pi-global-entry")}
                      </label>
                      <Select
                        value={draft.globalProviderEntry}
                        onValueChange={(v) => {
                          const next = String(v ?? "");
                          setField("globalProviderEntry", next);
                          onAutoSave();
                        }}
                      >
                        <SelectTrigger>
                          <SelectValue>
                            {(v: string | null) =>
                              v
                                ? entryLabel(
                                    globalProviderEntries.find(
                                      (e) => e.name === v
                                    )
                                  )
                                : ""
                            }
                          </SelectValue>
                        </SelectTrigger>
                        <SelectContent>
                          {globalProviderEntries.map((e) => (
                            <SelectItem key={e.name} value={e.name}>
                              {entryLabel(e)}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <p className="text-xs text-control-light">
                        {t("agent.acp-config-pi-global-entry-hint")}
                      </p>
                    </div>
                  ) : (
                    <p className="text-xs text-control-light">
                      {t("agent.acp-config-pi-global-entries-empty")}
                    </p>
                  ))}
                {selectedGlobalProvider?.providerType === "custom" &&
                  draft.globalProviderEntry &&
                  renderPiContextFields()}
              </>
            )}

            {draft.piMode === "self" && showLegacyInline && (
              <>
                <div className="flex flex-col gap-1">
                  <label className="text-sm font-medium">
                    {t("agent.acp-config-pi-api-provider")}
                  </label>
                  <Select
                    value={draft.apiProvider}
                    onValueChange={(v) => {
                      const next = String(v ?? "");
                      // Reset model when the API provider changes — the
                      // previous model belongs to the old provider's set.
                      // Clear the cached model list too (it is per
                      // provider) and cancel a pending key-change fetch;
                      // the user clicks Refresh to load the new list.
                      setDraft((d) => ({
                        ...d,
                        apiProvider: next,
                        model: "",
                        apiBaseUrl: next === "custom" ? d.apiBaseUrl : "",
                        contextWindow: next === "custom" ? d.contextWindow : 0,
                        maxTokens: next === "custom" ? d.maxTokens : 0,
                      }));
                      piModels.cancelPendingFetch();
                      piModels.clear();
                      onAutoSave();
                    }}
                  >
                    <SelectTrigger>
                      <SelectValue>{(v: string | null) => v ?? ""}</SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {piAPIProviderIds.map((id) => (
                        <SelectItem key={id} value={id}>
                          {id}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                {draft.apiProvider === "custom" && (
                  <div className="flex flex-col gap-1">
                    <label className="text-sm font-medium">
                      {t("agent.acp-config-pi-api-base-url")}
                    </label>
                    <Input
                      value={draft.apiBaseUrl}
                      onChange={(e) => setField("apiBaseUrl", e.target.value)}
                      onBlur={() => {
                        onAutoSave();
                        if (draftRef.current.apiBaseUrl.trim()) {
                          void piModels.fetchModels(
                            draftRef.current.apiProvider,
                            draftRef.current.apiKey,
                            draftRef.current.apiBaseUrl
                          );
                        }
                      }}
                      placeholder={t(
                        "agent.acp-config-pi-api-base-url-placeholder"
                      )}
                      spellCheck={false}
                    />
                    <p className="text-xs text-control-light">
                      {t("agent.acp-config-pi-api-base-url-hint")}
                    </p>
                  </div>
                )}

                <div className="flex flex-col gap-1">
                  <label className="text-sm font-medium">
                    {t("agent.acp-config-model")}
                  </label>
                  <div className="flex items-center gap-2">
                    <ModelCombobox
                      className="flex-1"
                      value={draft.model}
                      options={piModels.models}
                      loading={piModels.loading}
                      placeholder={t("agent.acp-config-pi-model-placeholder")}
                      disabled={!draft.apiProvider}
                      emptyLabel={t("agent.acp-config-pi-models-empty")}
                      onValueChange={(next) => setField("model", next)}
                    />
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={
                        !draft.apiProvider ||
                        piModels.loading ||
                        (draft.apiProvider === "deepseek" &&
                          draft.apiKey.trim() === "") ||
                        (draft.apiProvider === "custom" &&
                          draft.apiBaseUrl.trim() === "")
                      }
                      onClick={() => {
                        // Force a refetch: drop the cache entry first and
                        // cancel any pending key-change debounce.
                        piModels.cancelPendingFetch();
                        if (draft.apiProvider) {
                          piModels.invalidate(
                            draft.apiProvider,
                            draft.apiBaseUrl
                          );
                        }
                        void piModels.fetchModels(
                          draft.apiProvider,
                          draft.apiKey,
                          draft.apiBaseUrl
                        );
                      }}
                    >
                      {piModels.loading ? (
                        <Loader2 className="size-3.5 animate-spin" />
                      ) : (
                        t("agent.acp-config-pi-models-refresh")
                      )}
                    </Button>
                  </div>
                  {piModels.error && (
                    <p className="text-xs text-error">{piModels.error}</p>
                  )}
                </div>

                {draft.apiProvider === "custom" && renderPiContextFields()}

                <div className="flex flex-col gap-1">
                  <label className="text-sm font-medium">
                    {t("agent.acp-config-pi-api-key")}
                  </label>
                  <SecretInput
                    placeholder={t("agent.acp-config-pi-api-key-placeholder")}
                    value={draft.apiKey}
                    onChange={(e) => {
                      const next = e.target.value;
                      setField("apiKey", next);
                      // Fetch the model list once the user stops typing the
                      // key (debounced) — this is the "user changed the api
                      // key" trigger. deepseek needs the key; fetchModels
                      // no-ops for deepseek + empty key.
                      piModels.debouncedFetchModels(
                        draftRef.current.apiProvider,
                        next,
                        draftRef.current.apiBaseUrl
                      );
                    }}
                    onBlur={() => {
                      // Leaving the field: persist the key, and fetch
                      // immediately rather than waiting on the debounce.
                      piModels.cancelPendingFetch();
                      onAutoSave();
                      void piModels.fetchModels(
                        draftRef.current.apiProvider,
                        draftRef.current.apiKey,
                        draftRef.current.apiBaseUrl
                      );
                    }}
                  />
                  <p className="text-xs text-control-light">
                    {t("agent.acp-config-pi-api-key-hint")}
                  </p>
                </div>
              </>
            )}
          </>
        )}

        {selectedProviderInfo && !isPiRuntime && (
          <div className="flex flex-col gap-1">
            <label className="text-sm font-medium">
              {t("agent.acp-config-model")}
            </label>
            {providerSupportsModel && modelOptions.length > 0 ? (
              <div className="flex items-center gap-2">
                <div className="flex-1">
                  <Select
                    value={draft.model}
                    onValueChange={(v) => {
                      const next = String(v ?? "");
                      setField("model", next);
                      onAutoSave();
                    }}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue>
                        {(v: string | null) =>
                          v ? modelLabel(v, modelOptions) : ""
                        }
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {modelOptions.map((m) => (
                        <SelectItem key={m.value} value={m.value}>
                          {m.name || m.value}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={!canEdit || modelsRefreshing}
                  onClick={() => void refreshModels()}
                  title={t("agent.acp-config-models-refresh-hint")}
                >
                  {modelsRefreshing ? (
                    <Loader2 className="size-3.5 animate-spin" />
                  ) : (
                    t("agent.acp-config-models-refresh")
                  )}
                </Button>
              </div>
            ) : (
              <div className="flex flex-col gap-1">
                <p className="text-xs text-control-light">
                  {t("agent.acp-config-model-unsupported")}
                </p>
                <div className="flex items-center gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={!canEdit || modelsRefreshing}
                    onClick={() => void refreshModels()}
                    title={t("agent.acp-config-models-refresh-hint")}
                  >
                    {modelsRefreshing ? (
                      <Loader2 className="size-3.5 animate-spin" />
                    ) : (
                      t("agent.acp-config-models-refresh")
                    )}
                  </Button>
                </div>
              </div>
            )}
            {modelsRefreshError && (
              <p className="text-xs text-error">{modelsRefreshError}</p>
            )}
          </div>
        )}

        {isCustomProvider && !isPiRuntime && (
          <>
            <div className="flex flex-col gap-1">
              <label className="text-sm font-medium">
                {t("agent.acp-config-protocol")}
              </label>
              <Select
                value={draft.protocol}
                onValueChange={(v) => {
                  const next = String(v ?? "");
                  setField("protocol", next);
                  onAutoSave();
                }}
              >
                <SelectTrigger>
                  <SelectValue>
                    {(v: string | null) =>
                      t(
                        v === "acp-v2"
                          ? "agent.acp-config-protocol-v2"
                          : v === "acp-v1"
                            ? "agent.acp-config-protocol-v1"
                            : t("agent.acp-config-protocol-auto")
                      )
                    }
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="">
                    {t("agent.acp-config-protocol-auto")}
                  </SelectItem>
                  <SelectItem value="acp-v1">
                    {t("agent.acp-config-protocol-v1")}
                  </SelectItem>
                  <SelectItem value="acp-v2">
                    {t("agent.acp-config-protocol-v2")}
                  </SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-control-light">
                {t("agent.acp-config-protocol-hint")}
              </p>
            </div>

            <div className="flex flex-col gap-1">
              <label className="text-sm font-medium">
                {t("agent.acp-config-executable")}
              </label>
              <Input
                placeholder={t("agent.acp-config-executable-placeholder")}
                value={draft.executable}
                onChange={(e) => setField("executable", e.target.value)}
                onBlur={() => onAutoSave()}
              />
            </div>

            <StringListEditor
              label={t("agent.acp-config-args")}
              placeholder={t("agent.acp-config-args-placeholder")}
              values={draft.args}
              onChange={(next) => setField("args", next)}
              onCommit={(next) => {
                setField("args", next);
                onAutoSave();
              }}
            />
          </>
        )}

        {selectedProviderInfo && !isCustomProvider && !isPiRuntime && (
          <p className="text-xs text-control-light">
            {t("agent.acp-config-derived-command-hint")}
          </p>
        )}

        {!isPiRuntime && (
          <KeyValueEnvEditor
            label={t("agent.acp-config-custom-env")}
            entries={draft.customEnvEntries}
            onChange={(next) => setField("customEnvEntries", next)}
            onCommit={(next) => {
              setField("customEnvEntries", next);
              onAutoSave();
            }}
          />
        )}

        {!isPiRuntime && (
          <StringListEditor
            label={t("agent.acp-config-allow-env")}
            placeholder={t("agent.acp-config-allow-env-placeholder")}
            values={draft.allowEnv}
            onChange={(next) => setField("allowEnv", next)}
            onCommit={(next) => {
              setField("allowEnv", next);
              onAutoSave();
            }}
          />
        )}
      </div>
    </fieldset>
  );
  if (isCreate) {
    return body;
  }
  return (
    <Card title={t("agent.runtime-config")} actions={saveStatusActions}>
      {body}
    </Card>
  );
}

// Memoized so typing in the page-level identity/description/persona fields
// never re-renders the config form; typing inside it re-renders only this
// editor (the page re-renders only around saves and store loads).
export const AcpConfigEditor = memo(AcpConfigEditorImpl);
