import { create } from "@bufbuild/protobuf";
import {
  type Dispatch,
  type KeyboardEvent as ReactKeyboardEvent,
  type RefObject,
  type SetStateAction,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import { MAX_UPLOAD_BYTES, uploadFileToConversation } from "@/lib/file-upload";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import type { ChatMessageUI } from "@/stores/types";
import type { Attachment } from "@/types/proto-es/v1/command_pb";
import { AttachmentSchema } from "@/types/proto-es/v1/command_pb";
import { detectMention } from "./useMentionDetect";
import { type MentionTarget, targetToMention } from "./useMentionTargets";

// UploadItem tracks a file currently being uploaded so the composer can render
// a real progress bar instead of a generic spinner.
export interface UploadItem {
  id: string;
  name: string;
  progress: number;
  file: File;
  error?: string;
}

// ComposerDraft is the per-surface draft entry: half-typed text + completed
// attachments survive switching conversations/threads. In-flight uploads are
// deliberately NOT persisted — their completion closures belong to the
// composer instance that started them, so a finished upload can never land in
// another surface's composer (the cross-conversation upload bug).
export interface ComposerDraft {
  input: string;
  attachments: Attachment[];
}

// ComposerDraftsRef is the draft cache owned by the hosting surface (the chat
// page keys drafts by channelId; the thread panel keys drafts by root id).
export type ComposerDraftsRef = RefObject<Map<string, ComposerDraft>>;

// ComposerMentionState mirrors detectMention's result shape.
export interface ComposerMentionState {
  active: boolean;
  query: string;
  startIndex: number;
  matched: MentionTarget[];
}

// deriveMentions extracts the mention targets whose handles appear as @tokens
// in the draft (preceded by string start or whitespace, the same rule the
// popup's detection uses). Mentions are derived from the text rather than
// maintained as a parallel list: every input change recomputes them, so:
// - deleting a token (popup inactive at the time) also deletes the mention, and
// - a failed send that restores the text also restores its mentions.
function deriveMentions(
  input: string,
  targets: MentionTarget[]
): MentionTarget[] {
  if (targets.length === 0) return [];
  const found: MentionTarget[] = [];
  const re = /(?:^|\s)@(\S+)/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(input)) !== null) {
    const token = match[1];
    const target = targets.find((t) => t.handle === token);
    if (target && !found.includes(target)) found.push(target);
  }
  return found;
}

export interface UseChatComposerOptions {
  channelId: string;
  /** Set by the thread composer: sends become replies under this root. */
  threadRootId?: string;
  /** Per-surface draft isolation key (channelId, or the thread's root id). */
  draftKey: string;
  /** Draft cache owned by the hosting surface (chat page / thread panel). */
  draftsRef: ComposerDraftsRef;
  /** Enter sends (default) or Shift+Enter sends (inverted keybinding). */
  enterToSend: boolean;
  mentionTargets: MentionTarget[];
  /** Enables the as-task toggle + asTask send flag (channel composer only). */
  taskEnabled?: boolean;
  /** Fired once the optimistic row is appended (thread sticks to bottom). */
  onSendStart?: () => void;
  /** Textarea auto-resize cap in px (200 main / 160 thread). */
  textareaMaxHeight?: number;
}

export interface ChatComposerApi {
  input: string;
  sending: boolean;
  pendingAttachments: Attachment[];
  uploads: UploadItem[];
  removeUpload: (id: string) => void;
  removeAttachment: (id: string) => void;
  // The mention map the next send carries (derived from the input text).
  mentionMap: MentionTarget[];
  mentionState: ComposerMentionState | null;
  setMentionState: Dispatch<SetStateAction<ComposerMentionState | null>>;
  mentionSelectedIndex: number;
  setMentionSelectedIndex: Dispatch<SetStateAction<number>>;
  cursorPos: number;
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  fileInputRef: RefObject<HTMLInputElement | null>;
  // asTask toggle state; only meaningful when options.taskEnabled.
  asTask: boolean;
  setAsTask: Dispatch<SetStateAction<boolean>>;
  handleTextareaChange: (value: string, caretPos: number) => void;
  handleTextareaSelect: (caretPos: number) => void;
  handleTextareaKeyDown: (e: ReactKeyboardEvent<HTMLTextAreaElement>) => void;
  resetMentionState: () => void;
  insertMention: (target: MentionTarget) => void;
  send: () => Promise<void>;
  addFiles: (files: FileList | File[]) => Promise<void>;
  focusTextarea: () => void;
}

export function useChatComposer(opts: UseChatComposerOptions): ChatComposerApi {
  const {
    channelId,
    threadRootId,
    draftKey,
    draftsRef,
    enterToSend,
    mentionTargets,
    taskEnabled,
    onSendStart,
    textareaMaxHeight = 200,
  } = opts;
  const { t } = useTranslation();
  const conversationName = `conversations/${channelId}`;

  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>(
    []
  );
  const [uploads, setUploads] = useState<UploadItem[]>([]);
  const [mentionState, setMentionState] = useState<ComposerMentionState | null>(
    null
  );
  const [mentionSelectedIndex, setMentionSelectedIndex] = useState(0);
  const [cursorPos, setCursorPos] = useState(0);
  const [asTask, setAsTask] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  // Re-entrancy guard for send(): the `sending` state updates asynchronously,
  // so a fast double-Enter could otherwise start two sends (the second one
  // with no in-flight uploads -> the file would be dropped).
  const sendingRef = useRef(false);
  // The optimistic message id currently being sent (if any), so in-flight
  // upload progress can update the message bubble inline.
  const activeOptimisticIdRef = useRef<string | null>(null);
  // Upload ids adopted by an optimistic send: their completion must not
  // re-add to the composer's pendingAttachments (already cleared).
  const adoptedUploadIdsRef = useRef<Set<string>>(new Set());
  // Uploads still in flight, stored as promises resolving to their Attachment
  // so send can wait for them before composing the message.
  const inFlightUploadsRef = useRef<Promise<Attachment | null>[]>([]);

  // ---- Mention derivation --------------------------------------------------
  // The outgoing mention list derives from the input text (one regex pass):
  // tokens deleted outside an active popup still drop their pending mention,
  // and clearing the composer (send start) clears mentions for free.
  const mentionMap = useMemo(
    () => deriveMentions(input, mentionTargets),
    [input, mentionTargets]
  );

  // ---- Optimistic store writes --------------------------------------------
  // Slice actions route every optimistic mutation so the invariants (thread
  // snapshot creation on a racing send, id dedup, same-reference bail-outs)
  // live in exactly one place instead of component-level setState surgery.
  const write = useMemo(
    () => ({
      append: (msg: ChatMessageUI) => {
        const state = useAppStore.getState();
        if (threadRootId) state.appendThreadMessage(threadRootId, msg);
        else state.appendChatMessage(conversationName, msg);
      },
      patch: (messageId: string, patch: Partial<ChatMessageUI>) => {
        const state = useAppStore.getState();
        if (threadRootId)
          state.patchThreadMessage(threadRootId, messageId, patch);
        else state.patchChatMessage(conversationName, messageId, patch);
      },
      remove: (messageId: string) => {
        const state = useAppStore.getState();
        if (threadRootId) state.removeThreadMessage(threadRootId, messageId);
        else state.removeChatMessage(conversationName, messageId);
      },
    }),
    [threadRootId, conversationName]
  );

  // Mirror an in-flight upload's progress into the adopted optimistic message
  // bubble (per-attachment upload percentage rendered by MessageRow).
  const mirrorUploadProgress = useCallback(
    (uploadId: string, progress: number) => {
      const optimisticId = activeOptimisticIdRef.current;
      if (!optimisticId || !adoptedUploadIdsRef.current.has(uploadId)) return;
      const state = useAppStore.getState();
      const msg = threadRootId
        ? state.threadByRoot[threadRootId]?.messages.find(
            (m) => m.id === optimisticId
          )
        : state.chatMessages[conversationName]?.find(
            (m) => m.id === optimisticId
          );
      if (!msg) return;
      const key = `pending-${uploadId}`;
      if (msg.uploadProgress?.[key] === progress) return;
      write.patch(optimisticId, {
        uploadProgress: {
          ...(msg.uploadProgress ?? {}),
          [key]: progress,
        },
      });
    },
    [threadRootId, conversationName]
  );

  // ---- Draft cache ---------------------------------------------------------
  // Restore the entering surface's draft. Declared before the persist effect
  // (the original page's rule): this runs once per mounted surface — the host
  // keys the composer per draftKey — and any stale write re-triggers persist,
  // writing the restored values back.
  useEffect(() => {
    const d = draftsRef.current?.get(draftKey);
    setInput(d?.input ?? "");
    setPendingAttachments(d?.attachments ?? []);
    setMentionState(null);
    setMentionSelectedIndex(0);
    setCursorPos(0);
    setAsTask(false);
  }, [draftKey, draftsRef]);

  // NOTE(draft): in-flight uploads are intentionally dropped on restore; see
  // ComposerDraft.

  // Persist the current input/attachments on every change so a switch keeps
  // its draft.
  useEffect(() => {
    draftsRef.current?.set(draftKey, {
      input,
      attachments: pendingAttachments,
    });
  }, [draftKey, input, pendingAttachments, draftsRef]);

  // ---- Textarea behaviors --------------------------------------------------
  const autoResize = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, textareaMaxHeight)}px`;
  }, [textareaMaxHeight]);
  useEffect(() => {
    autoResize();
  }, [input, autoResize]);

  const handleTextareaChange = useCallback(
    (value: string, caretPos: number) => {
      setInput(value);
      setCursorPos(caretPos);
      setMentionState(detectMention(value, caretPos, mentionTargets));
      setMentionSelectedIndex(0);
    },
    [mentionTargets]
  );

  const handleTextareaSelect = useCallback(
    (caretPos: number) => {
      setCursorPos(caretPos);
      setMentionState(detectMention(input, caretPos, mentionTargets));
      setMentionSelectedIndex(0);
    },
    [input, mentionTargets]
  );

  const resetMentionState = useCallback(() => setMentionState(null), []);

  const insertMention = useCallback(
    (target: MentionTarget) => {
      if (!mentionState) return;
      const before = input.slice(0, mentionState.startIndex);
      const after = input.slice(cursorPos);
      setInput(`${before}@${target.handle} ${after}`);
      // The map re-derives from the restored @token text (deriveMentions).
      setMentionState(null);
      setMentionSelectedIndex(0);
      setTimeout(() => {
        const el = textareaRef.current;
        if (el) {
          const newPos = mentionState.startIndex + target.handle.length + 2;
          el.focus();
          el.setSelectionRange(newPos, newPos);
        }
      }, 0);
    },
    [input, cursorPos, mentionState]
  );

  // ---- Uploads -------------------------------------------------------------
  const addFiles = useCallback(
    async (files: FileList | File[]) => {
      const list = Array.from(files);
      if (list.length === 0) return;
      // Upload in parallel; each file gets its own progress chip. The browser
      // streams the multipart bodies natively.
      const tasks = list.map((file) => {
        const id = `${file.name}-${Date.now()}-${Math.random()}`;
        // Reject oversized files before starting the upload so the user sees
        // a clear error instead of a mid-upload failure.
        if (file.size > MAX_UPLOAD_BYTES) {
          toastManager.add({ type: "error", title: t("chat.file-too-large") });
          return Promise.resolve(null);
        }
        setUploads((prev) => [
          ...prev,
          { id, name: file.name, progress: 0, file },
        ]);
        const task = uploadFileToConversation({
          conversation: conversationName,
          originalName: file.name,
          mimeType: file.type || "",
          file,
          onProgress: (p) => {
            const progress = p.percent;
            setUploads((prev) =>
              prev.map((u) => (u.id === id ? { ...u, progress } : u))
            );
            // If this upload was adopted by an optimistic send, mirror the
            // progress into that message bubble in the chat/thread list.
            mirrorUploadProgress(id, progress);
          },
        })
          .then((att) => {
            if (att && !adoptedUploadIdsRef.current.has(id)) {
              setPendingAttachments((prev) => [...prev, att]);
            }
            return att;
          })
          .catch((err) => {
            // Keep the failed upload visible with an error so it doesn't
            // silently vanish; the user can dismiss it manually.
            console.error("file upload failed", err);
            const message =
              err instanceof Error
                ? err.message
                : String(err ?? "upload failed");
            setUploads((prev) =>
              prev.map((u) => (u.id === id ? { ...u, error: message } : u))
            );
            return null;
          })
          .finally(() => {
            // Remove only successful uploads; failed ones stay visible with
            // their error so the user can see what went wrong.
            setUploads((prev) => prev.filter((u) => u.id !== id || u.error));
          });
        inFlightUploadsRef.current.push(task);
        return task;
      });
      await Promise.all(tasks);
    },
    [conversationName, mirrorUploadProgress]
  );

  // removeUpload drops a (failed) upload chip; removeAttachment drops a
  // completed attachment chip.
  const removeUpload = useCallback(
    (id: string) => setUploads((prev) => prev.filter((p) => p.id !== id)),
    []
  );
  const removeAttachment = useCallback((id: string) => {
    setPendingAttachments((prev) => prev.filter((p) => p.id !== id));
  }, []);

  // ---- Send ----------------------------------------------------------------
  const send = useCallback(async () => {
    const text = input.trim();
    if (sendingRef.current || !channelId) return;
    sendingRef.current = true;

    const completedAttachments = pendingAttachments;
    const inFlight = inFlightUploadsRef.current;
    const hasInFlight = inFlight.length > 0;
    const tempId = crypto.randomUUID();

    // Build the optimistic message: completed attachments plus temp
    // placeholders for files still uploading, so the whole message (text +
    // files) appears in the chat immediately with a "sending" state.
    const tempAttachments: Attachment[] = [
      ...completedAttachments,
      ...uploads.map((u) =>
        create(AttachmentSchema, {
          id: `pending-${u.id}`,
          name: u.name,
          mimeType: u.file.type || "",
          sizeBytes: BigInt(u.file.size),
        })
      ),
    ];
    const progressMap: Record<string, number> = {};
    for (const u of uploads) progressMap[`pending-${u.id}`] = u.progress;

    const optimisticMsg: ChatMessageUI = {
      id: tempId,
      role: "user",
      content: text,
      timestamp: new Date(),
      attachments: tempAttachments,
      sending: true,
      uploadProgress: hasInFlight ? progressMap : undefined,
    };

    write.append(optimisticMsg);

    // Clear the composer right away so the user can start typing the next
    // message while uploads continue in the background. Mentions follow from
    // the text (derived), so they clear with it.
    setInput("");
    setMentionState(null);
    setPendingAttachments([]);
    if (taskEnabled) setAsTask(false);
    setSending(true);
    onSendStart?.();
    activeOptimisticIdRef.current = tempId;

    // Adopt the in-flight uploads: their completion now updates the optimistic
    // message, not the (already cleared) composer pending list.
    for (const u of uploads) adoptedUploadIdsRef.current.add(u.id);
    inFlightUploadsRef.current = [];
    setUploads([]);

    let finalAttachments = completedAttachments;
    if (hasInFlight) {
      const results = await Promise.all(inFlight);
      const uploaded = results.filter((a): a is Attachment => a !== null);
      // Dedupe by id: an upload that finished just before send may already be
      // in completedAttachments AND in the resolved in-flight results.
      finalAttachments = [
        ...new Map(
          [...completedAttachments, ...uploaded].map((a) => [a.id, a])
        ).values(),
      ];
      // Replace the temp placeholders with the real attachments.
      write.patch(tempId, {
        attachments: finalAttachments,
        uploadProgress: undefined,
      });
    }

    if (!text && finalAttachments.length === 0) {
      // Nothing to send (e.g. all uploads failed) — remove the optimistic row.
      write.remove(tempId);
      setSending(false);
      sendingRef.current = false;
      activeOptimisticIdRef.current = null;
      return;
    }

    const sendAsTask = asTask;
    const mentions = mentionMap.map(targetToMention);
    const state = useAppStore.getState();
    try {
      if (threadRootId) {
        await state.sendThreadMessage(
          channelId,
          threadRootId,
          text,
          mentions,
          finalAttachments,
          tempId
        );
      } else {
        await state.sendChannelMessage(
          channelId,
          text,
          mentions,
          finalAttachments,
          sendAsTask,
          tempId
        );
      }
    } catch {
      // Send failed — remove the optimistic row and restore the composer so
      // the user can retry (bug fix: the half-typed text used to stay lost;
      // the derived mention map follows the restored text).
      write.remove(tempId);
      setInput(text);
      setPendingAttachments(finalAttachments);
      if (taskEnabled) setAsTask(sendAsTask);
    } finally {
      setSending(false);
      sendingRef.current = false;
      activeOptimisticIdRef.current = null;
      setTimeout(() => textareaRef.current?.focus(), 0);
    }
  }, [
    input,
    uploads,
    pendingAttachments,
    mentionMap,
    asTask,
    taskEnabled,
    channelId,
    threadRootId,
    conversationName,
    onSendStart,
    write,
    t,
  ]);

  // handleTextareaKeyDown drives the mention popup's keyboard navigation and
  // the per-user send keybinding (Enter by default, Shift+Enter when the user
  // inverted it in settings).
  const handleTextareaKeyDown = useCallback(
    (e: ReactKeyboardEvent<HTMLTextAreaElement>) => {
      if (mentionState?.active) {
        const total = mentionState.matched.length;
        if (e.key === "ArrowDown") {
          e.preventDefault();
          setMentionSelectedIndex((idx) => (idx + 1 < total ? idx + 1 : 0));
          return;
        }
        if (e.key === "ArrowUp") {
          e.preventDefault();
          setMentionSelectedIndex((idx) =>
            idx - 1 >= 0 ? idx - 1 : total - 1
          );
          return;
        }
        if (e.key === "Enter" || e.key === "Tab") {
          if (total === 0) return;
          e.preventDefault();
          if (mentionState.matched[mentionSelectedIndex]) {
            insertMention(mentionState.matched[mentionSelectedIndex]);
          }
          return;
        }
        if (e.key === "Escape") {
          e.preventDefault();
          setMentionState(null);
          return;
        }
      }
      if (e.nativeEvent.isComposing) return;
      if (e.key !== "Enter") return;
      const wantSend = enterToSend ? !e.shiftKey : e.shiftKey;
      if (wantSend) {
        e.preventDefault();
        void send();
      }
    },
    [mentionState, insertMention, enterToSend, send]
  );

  const focusTextarea = useCallback(() => textareaRef.current?.focus(), []);

  return {
    input,
    sending,
    pendingAttachments,
    uploads,
    mentionMap,
    mentionState,
    setMentionState,
    mentionSelectedIndex,
    setMentionSelectedIndex,
    cursorPos,
    textareaRef,
    fileInputRef,
    asTask,
    setAsTask,
    handleTextareaChange,
    handleTextareaSelect,
    handleTextareaKeyDown,
    resetMentionState,
    insertMention,
    send,
    addFiles,
    removeUpload,
    removeAttachment,
    focusTextarea,
  };
}
