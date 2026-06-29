// A clickable @mention chip rendered inline in message bodies. It is a span
// (inline element) styled as a pill, so it carries `role="button"` +
// `tabIndex={0}` and activates on Enter and Space to stay keyboard-accessible.
export function MentionBadge({
  name,
  onClick,
}: {
  name: string;
  onClick: () => void;
}) {
  return (
    <span
      role="button"
      tabIndex={0}
      className="inline-flex items-center px-1 py-0.5 rounded bg-accent/15 text-accent font-medium cursor-pointer hover:bg-accent/25"
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick();
        }
      }}
    >
      @{name}
    </span>
  );
}
