// Time formatting helpers shared across pages. They live apart from the
// command-status lookups because every list/page renders timestamps while only
// the status badge needs the enum→key tables.

export function formatDuration(ms: number | bigint | undefined): string {
  if (ms === undefined || ms === 0n) return "-";
  const num = Number(ms);
  if (num < 1000) return `${num}ms`;
  if (num < 60000) return `${(num / 1000).toFixed(1)}s`;
  return `${(num / 60000).toFixed(1)}m`;
}

export function formatTimestamp(ts: { seconds?: bigint } | undefined): string {
  if (!ts?.seconds) return "-";
  return new Date(Number(ts.seconds) * 1000).toLocaleString();
}

// formatActivityListTime returns a compact representation for the activity feed
// on small screens: time-of-day for today, otherwise "M/D time".
export function formatActivityListTime(
  ts:
    | {
        seconds?: bigint;
      }
    | undefined
): {
  time: string;
  date: string;
} {
  if (!ts?.seconds) return { time: "", date: "" };
  const date = new Date(Number(ts.seconds) * 1000);
  const now = new Date();
  const isToday = date.toDateString() === now.toDateString();
  const time = date.toLocaleTimeString([], {
    hour: "numeric",
    minute: "2-digit",
  });
  if (isToday) return { time, date: "" };
  return {
    time,
    date: date.toLocaleDateString([], { month: "numeric", day: "numeric" }),
  };
}

// formatConversationListTime returns the compact timestamp for the
// conversation-list preview: "HH:MM" for today, "M/D" for earlier in the
// current year, and "YYYY/M/D" for messages from a previous year. Manual
// formatting keeps the label locale-stable next to the mixed-language list,
// and the same year/day split the user sees in the message timeline.
export function formatConversationListTime(ms: number | undefined): string {
  if (ms === undefined || Number.isNaN(ms)) return "";
  const date = new Date(ms);
  if (Number.isNaN(date.getTime())) return "";
  const now = new Date();
  if (date.toDateString() === now.toDateString()) {
    const hh = String(date.getHours()).padStart(2, "0");
    const mm = String(date.getMinutes()).padStart(2, "0");
    return `${hh}:${mm}`;
  }
  const month = String(date.getMonth() + 1);
  const day = String(date.getDate());
  if (date.getFullYear() === now.getFullYear()) return `${month}/${day}`;
  return `${date.getFullYear()}/${month}/${day}`;
}
