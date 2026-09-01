import { code } from "@streamdown/code";
import {
  Streamdown,
  type StreamdownProps,
  type StreamdownTranslations,
} from "streamdown";
import { cn } from "@/lib/utils";

export type MarkdownVariant =
  | "chat"
  | "preview"
  | "workspace"
  | "command"
  | "summary";

export interface MarkdownRendererProps {
  content: string;
  variant?: MarkdownVariant;
  className?: string;
  mentionAware?: boolean;
  mode?: "static" | "streaming";
  isAnimating?: boolean;
}

const mentionAllowedTags = {
  mention: ["type", "id", "name", "label"],
};

const translations: Partial<StreamdownTranslations> = {
  copyCode: "Copy code",
  copied: "Copied",
  downloadFile: "Download file",
};

interface MentionChipProps {
  type?: string;
  id?: string;
  name?: string;
  label?: string;
}

function MentionChip({
  type = "",
  id = "",
  name = "",
  label,
}: MentionChipProps) {
  const displayName = label ?? name;
  return (
    <span
      data-mtype={type}
      data-mid={id}
      data-mname={name}
      role="button"
      tabIndex={0}
      className="mention-chip"
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          e.currentTarget.click();
        }
      }}
    >
      @{displayName}
    </span>
  );
}

const mentionComponents = {
  mention: MentionChip,
};

const baseProps: Pick<
  StreamdownProps,
  "plugins" | "controls" | "lineNumbers" | "translations"
> = {
  plugins: { code },
  controls: {
    code: {
      copy: true,
      download: false,
    },
    // Streamdown's fullscreen table control portals to document.body at z-50,
    // behind the app's z-2500 preview layer. Keep table copy controls, but
    // disable the inaccessible fullscreen surface.
    table: {
      fullscreen: false,
    },
  },
  lineNumbers: false,
  translations,
};

function variantClassName(variant: MarkdownVariant): string {
  return variant === "chat"
    ? "markdown-content markdown-content-chat"
    : "markdown-content";
}

export function MarkdownRenderer({
  content,
  variant = "chat",
  className,
  mentionAware = false,
  mode = "static",
  isAnimating = false,
}: MarkdownRendererProps) {
  return (
    <div className={cn(variantClassName(variant), className)}>
      <Streamdown
        {...baseProps}
        mode={mode}
        isAnimating={mode === "streaming" && isAnimating}
        allowedTags={mentionAware ? mentionAllowedTags : undefined}
        literalTagContent={mentionAware ? ["mention"] : undefined}
        components={mentionAware ? mentionComponents : undefined}
      >
        {content}
      </Streamdown>
    </div>
  );
}

export function FinalSummary({
  content,
  className,
}: {
  content: string;
  className?: string;
}) {
  return (
    <MarkdownRenderer
      content={content}
      variant="summary"
      className={className}
    />
  );
}
