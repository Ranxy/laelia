import { useCallback, useRef, useState } from "react";
import { isPiProvider } from "@/components/profile-common";
import {
  foldCustomEnv,
  stringifyConfigForComparison,
  toOptionalBigInt,
} from "@/lib/acp-config-draft";
import type { AgentACPConfigInput } from "@/stores/types";
import type {
  AgentACPConfig,
  AgentProviderInfo,
} from "@/types/proto-es/v1/agent_pb";

// Shared draft model for the ACP runtime-config editor (agent profile page;
// the machine profile page migrates later). piMode is UI-only state — it is
// never sent to the server, it just drives which pi key-source fields show.
export type AcpPiMode = "own" | "global" | "self";

export interface AcpConfigDraft {
  executable: string;
  args: string[];
  allowEnv: string[];
  provider: string;
  model: string;
  protocol: string;
  apiProvider: string;
  apiKey: string;
  apiBaseUrl: string;
  contextWindow: number;
  maxTokens: number;
  piMode: AcpPiMode;
  globalProvider: string;
  globalProviderEntry: string;
  customEnvEntries: { key: string; value: string }[];
}

export function emptyAcpConfigDraft(): AcpConfigDraft {
  return {
    executable: "",
    args: [],
    allowEnv: [],
    provider: "",
    model: "",
    protocol: "",
    apiProvider: "",
    apiKey: "",
    apiBaseUrl: "",
    contextWindow: 0,
    maxTokens: 0,
    piMode: "global",
    globalProvider: "",
    globalProviderEntry: "",
    customEnvEntries: [],
  };
}

// derivePiMode seeds the pi key-source mode from a persisted config, in the
// same order the page's seed effect used: user-installed pi without any
// key-source is "own"; a managed global provider is "global"; an inline api
// provider is "self"; otherwise "global".
export function derivePiMode(
  provider: string,
  globalProvider: string,
  apiProvider: string
): AcpPiMode {
  if (provider === "pi" && !globalProvider && !apiProvider) return "own";
  if (globalProvider) return "global";
  if (apiProvider) return "self";
  return "global";
}

// draftFromPersisted seeds a draft from the agent's persisted config. The key
// is seeded too so an editor can see/keep it; non-editors get an empty key
// server-side (redacted), which is fine — they cannot save anyway. On save an
// empty key means "keep existing".
export function draftFromPersisted(
  cfg: AgentACPConfig | undefined
): AcpConfigDraft {
  return {
    executable: cfg?.executable ?? "",
    args: cfg?.args ? [...cfg.args] : [],
    allowEnv: cfg?.allowEnv ? [...cfg.allowEnv] : [],
    provider: cfg?.provider ?? "",
    model: cfg?.model ?? "",
    protocol: cfg?.protocol ?? "",
    apiProvider: cfg?.apiProvider ?? "",
    apiKey: cfg?.apiKey ?? "",
    apiBaseUrl: cfg?.apiBaseUrl ?? "",
    contextWindow: cfg?.contextWindow ? Number(cfg.contextWindow) : 0,
    maxTokens: cfg?.maxTokens ? Number(cfg.maxTokens) : 0,
    globalProvider: cfg?.globalProvider ?? "",
    globalProviderEntry: cfg?.globalProviderEntry ?? "",
    customEnvEntries: cfg?.customEnv
      ? Object.entries(cfg.customEnv).map(([key, value]) => ({ key, value }))
      : [],
    piMode: derivePiMode(
      cfg?.provider ?? "",
      cfg?.globalProvider ?? "",
      cfg?.apiProvider ?? ""
    ),
  };
}

// draftToInput builds a full-replace AgentACPConfigInput from a draft,
// carrying the given persona (the persisted persona for config auto-saves, so
// an unsaved persona draft is never persisted by a config save). Strings are
// trimmed, empty strings dropped from args/allowEnv, protocol is custom-only,
// customEnv is folded from the key-value entries, and an empty apiKey passes
// through as-is ("keep the existing stored key" server-side).
export function draftToInput(
  draft: AcpConfigDraft,
  personaPrompt: string
): AgentACPConfigInput {
  return {
    executable: draft.executable.trim(),
    args: draft.args.map((a) => a.trim()).filter((a) => a !== ""),
    allowEnv: draft.allowEnv.map((e) => e.trim()).filter((e) => e !== ""),
    provider: draft.provider.trim(),
    model: draft.model.trim(),
    // protocol is only meaningful for a custom provider; a built-in
    // provider's protocol is fixed by its implementation.
    protocol: draft.provider === "custom" ? draft.protocol.trim() : "",
    customEnv: foldCustomEnv(draft.customEnvEntries),
    personaPrompt,
    apiProvider: draft.apiProvider.trim(),
    apiKey: draft.apiKey,
    apiBaseUrl: draft.apiBaseUrl.trim(),
    contextWindow: toOptionalBigInt(draft.contextWindow),
    maxTokens: toOptionalBigInt(draft.maxTokens),
    globalProvider: draft.globalProvider.trim(),
    globalProviderEntry: draft.globalProviderEntry.trim(),
  };
}

// persistedToInput builds a full-replace config payload from the persisted
// server config, overriding only persona — so a persona save never touches
// (possibly mid-edit, possibly invalid) config draft state.
export function persistedToInput(
  cfg: AgentACPConfig | undefined,
  personaPrompt: string
): AgentACPConfigInput {
  return {
    executable: cfg?.executable ?? "",
    args: cfg?.args ? [...cfg.args] : [],
    allowEnv: cfg?.allowEnv ? [...cfg.allowEnv] : [],
    provider: cfg?.provider ?? "",
    model: cfg?.model ?? "",
    protocol: cfg?.protocol ?? "",
    customEnv: { ...(cfg?.customEnv ?? {}) },
    personaPrompt,
    apiProvider: cfg?.apiProvider ?? "",
    // Preserve the stored key on a persona-only save.
    apiKey: cfg?.apiKey ?? "",
    apiBaseUrl: cfg?.apiBaseUrl ?? "",
    contextWindow:
      cfg?.contextWindow && cfg.contextWindow > 0n
        ? cfg.contextWindow
        : undefined,
    maxTokens: cfg?.maxTokens && cfg.maxTokens > 0n ? cfg.maxTokens : undefined,
    globalProvider: cfg?.globalProvider ?? "",
    globalProviderEntry: cfg?.globalProviderEntry ?? "",
  };
}

// canSaveFor: a config is saveable once a provider (built-in, custom, or
// builtin-pi) is chosen. For the custom path an executable is still required;
// for a built-in provider the command is derived from the registry, so
// executable stays empty. When the provider exposes model selection, a model
// must also be chosen. For builtin-pi, an api provider + model are required;
// the api key is optional on save (empty means keep the existing stored key).
export function canSaveFor(
  draft: AcpConfigDraft,
  availableProviders: AgentProviderInfo[]
): boolean {
  if (draft.provider === "custom") return draft.executable.trim() !== "";
  if (isPiProvider(draft.provider)) {
    // User-installed pi may use pi's own model/auth: only a model is needed.
    if (
      draft.provider === "pi" &&
      !draft.globalProvider &&
      !draft.apiProvider
    ) {
      return draft.model.trim() !== "";
    }
    // Global-provider mode needs a provider + entry; self-provided mode
    // needs an api provider + model.
    if (draft.globalProvider) {
      return draft.globalProviderEntry.trim() !== "";
    }
    return draft.apiProvider.trim() !== "" && draft.model.trim() !== "";
  }
  const info = availableProviders.find((p) => p.providerId === draft.provider);
  const needsModel =
    !!info?.supportsModelConfigOption && (info?.models ?? []).length > 0;
  return draft.provider !== "" && (!needsModel || draft.model.trim() !== "");
}

// useAcpConfigDraft owns the runtime-config draft: a single draft object in
// state plus a synchronous ref mirror. Every setter updates both, so async
// save closures read the CURRENT draft (replacing the page's old
// state+configRef dual-write). The draft is keyed/remounted per agent by the
// owner to get seed-only-on-agent-change semantics.
export function useAcpConfigDraft(
  initial: AcpConfigDraft = emptyAcpConfigDraft()
) {
  const [draft, setDraftState] = useState(initial);
  const draftRef = useRef(draft);

  // setDraft replaces the whole draft (object or updater); the ref mirror is
  // written first so a save enqueued in the same tick reads the new values.
  const setDraft = useCallback(
    (next: AcpConfigDraft | ((prev: AcpConfigDraft) => AcpConfigDraft)) => {
      const resolved =
        typeof next === "function" ? next(draftRef.current) : next;
      draftRef.current = resolved;
      setDraftState(resolved);
    },
    []
  );

  const setField = useCallback(
    <K extends keyof AcpConfigDraft>(key: K, value: AcpConfigDraft[K]) => {
      setDraft({ ...draftRef.current, [key]: value });
    },
    [setDraft]
  );

  // Full-replace input from the CURRENT draft (reads the ref mirror, so it is
  // safe inside async save closures).
  const toInput = useCallback(
    (personaPrompt: string) => draftToInput(draftRef.current, personaPrompt),
    []
  );

  const canSave = useCallback(
    (availableProviders: AgentProviderInfo[]) =>
      canSaveFor(draftRef.current, availableProviders),
    []
  );

  // isDirtyAgainst compares the current draft against a persisted config,
  // taking personaPrompt from the persisted config (never the draft) — the
  // page's isConfigDirty semantics.
  const isDirtyAgainst = useCallback(
    (persisted: AgentACPConfig | undefined) => {
      const persona = persisted?.personaPrompt ?? "";
      const draftInput = toInput(persona);
      const persistedInput = persistedToInput(persisted, persona);
      return (
        stringifyConfigForComparison(draftInput) !==
        stringifyConfigForComparison(persistedInput)
      );
    },
    [toInput]
  );

  return {
    draft,
    draftRef,
    setField,
    setDraft,
    toInput,
    canSave,
    isDirtyAgainst,
  };
}
