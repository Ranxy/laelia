import { useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useShallow } from "zustand/react/shallow";
import { Badge } from "@/react/components/ui/badge";
import { Button } from "@/react/components/ui/button";
import { Input } from "@/react/components/ui/input";
import { useAppStore } from "@/react/stores";
import type { ChatMessage } from "@/react/stores/types";
import { CommandStatus } from "@/types/proto-es/v1/command_pb";

const statusLabels: Record<number, string> = {
  [CommandStatus.PENDING]: "Pending",
  [CommandStatus.RUNNING]: "Running",
  [CommandStatus.COMPLETED]: "Completed",
  [CommandStatus.FAILED]: "Failed",
  [CommandStatus.CANCELLED]: "Cancelled",
  [CommandStatus.TIMEOUT]: "Timeout",
};

const statusVariants: Record<
  number,
  "default" | "secondary" | "success" | "warning" | "destructive"
> = {
  [CommandStatus.PENDING]: "secondary",
  [CommandStatus.RUNNING]: "warning",
  [CommandStatus.COMPLETED]: "success",
  [CommandStatus.FAILED]: "destructive",
  [CommandStatus.CANCELLED]: "destructive",
  [CommandStatus.TIMEOUT]: "destructive",
};

function formatTime(date: Date): string {
  const hours = date.getHours().toString().padStart(2, "0");
  const minutes = date.getMinutes().toString().padStart(2, "0");
  return `${hours}:${minutes}`;
}

export function ChatPage() {
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();
  const agent = `agents/${agentId}`;

  const chatMessages = useAppStore(
    useShallow((s) => s.chatMessages[agent] ?? [])
  );
  const chatLoading = useAppStore((s) => s.chatLoading);

  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [waitingCommand, setWaitingCommand] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const lastAgentRef = useRef<string | null>(null);

  useEffect(() => {
    if (lastAgentRef.current === agent) return;
    lastAgentRef.current = agent;
    useAppStore.getState().loadChatHistory(agent, 100);
  }, [agent]);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [chatMessages]);

  const handleSend = async () => {
    const text = input.trim();
    if (!text || sending || waitingCommand) return;
    setInput("");
    setSending(true);
    setWaitingCommand(true);

    try {
      const res = await useAppStore.getState().sendCommand(agent, text, {
        executorKind: 2,
        instruction: text,
        source: 2,
      });

      if (res.name) {
        const checkCommand = async () => {
          try {
            const cmd = await useAppStore.getState().getCommand(res.name);
            if (!cmd) return;

            if (
              cmd.status !== CommandStatus.PENDING &&
              cmd.status !== CommandStatus.RUNNING
            ) {
              if (pollRef.current) {
                clearInterval(pollRef.current);
                pollRef.current = null;
              }
              setWaitingCommand(false);
              useAppStore.getState().loadChatHistory(agent, 100);
            }
          } catch {
            // retry on next interval
          }
        };

        pollRef.current = setInterval(checkCommand, 1000);
      } else {
        setWaitingCommand(false);
      }
    } finally {
      setSending(false);
    }
  };

  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, []);

  const lastMsg = chatMessages[chatMessages.length - 1];
  const isWaiting = waitingCommand && lastMsg?.role === "user";

  return (
    <div className="p-6 flex flex-col h-[calc(100vh-4rem)] max-w-3xl mx-auto w-full">
      <div className="flex items-center justify-between mb-4 shrink-0">
        <h1 className="text-xl font-semibold text-main">Chat with {agentId}</h1>
        <Button
          variant="outline"
          size="sm"
          onClick={() => navigate(`/agents/${agentId}/commands`)}
        >
          Tasks
        </Button>
      </div>

      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto flex flex-col gap-3 pb-4"
      >
        {chatLoading && (
          <div className="text-center text-control-light py-8">Loading...</div>
        )}
        {!chatLoading && chatMessages.length === 0 && (
          <div className="text-center text-control-light py-12">
            Start a conversation with the agent
          </div>
        )}
        {chatMessages.map((msg: ChatMessage) => (
          <ChatBubble
            key={msg.id}
            msg={msg}
            onViewDetails={() => {
              if (msg.commandName) {
                navigate(
                  `/agents/${agentId}/commands/${msg.commandName.split("/").pop()}`
                );
              }
            }}
          />
        ))}
        {isWaiting && (
          <div className="text-sm text-control-light pl-4">
            Agent is thinking...
          </div>
        )}
      </div>

      <div className="shrink-0 flex gap-2 pt-2 border-t border-control-border">
        <Input
          className="flex-1"
          placeholder={
            isWaiting ? "Agent is processing..." : "Type a message..."
          }
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              handleSend();
            }
          }}
          disabled={isWaiting}
        />
        <Button
          onClick={handleSend}
          disabled={sending || waitingCommand || !input.trim()}
        >
          Send
        </Button>
      </div>
    </div>
  );
}

function ChatBubble({
  msg,
  onViewDetails,
}: {
  msg: ChatMessage;
  onViewDetails: () => void;
}) {
  const isUser = msg.role === "user";

  return (
    <div className={`flex flex-col ${isUser ? "items-end" : "items-start"}`}>
      <div className="flex items-center gap-2 mb-0.5">
        <span className="text-xs text-control-light">
          {isUser ? "You" : "Agent"}
        </span>
        <span className="text-xs text-control-light/60">
          {formatTime(msg.timestamp)}
        </span>
        {msg.role === "assistant" && msg.status !== undefined && (
          <Badge
            variant={statusVariants[msg.status] ?? "default"}
            className="text-[10px] px-1 py-0"
          >
            {statusLabels[msg.status] ?? ""}
          </Badge>
        )}
      </div>
      <div
        className={`max-w-[80%] rounded-lg px-3 py-2 text-sm whitespace-pre-wrap ${
          isUser ? "bg-primary text-primary-foreground" : "bg-accent text-main"
        }`}
      >
        {msg.content || "(empty)"}
      </div>
      {msg.role === "assistant" && msg.commandName && (
        <button
          type="button"
          className="text-xs text-control-light hover:text-main mt-0.5 cursor-pointer"
          onClick={onViewDetails}
        >
          View details &rarr;
        </button>
      )}
    </div>
  );
}
