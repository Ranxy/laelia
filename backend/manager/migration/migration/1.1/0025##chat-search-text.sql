-- SearchChatHistory searches a markdown-stripped plain-text copy of each
-- message (search_text) so queries match the rendered text rather than the raw
-- markdown. Populated on write; existing rows keep '' (no backfill).
-- Version: 1.1.25
ALTER TABLE chat_message ADD COLUMN IF NOT EXISTS search_text TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_chat_message_search_text_trgm
    ON chat_message USING GIN (search_text gin_trgm_ops);
CREATE OR REPLACE FUNCTION chat_occurrences(haystack text, needle text)
RETURNS integer
LANGUAGE sql
IMMUTABLE
AS $$
  SELECT (length(lower(haystack)) - length(replace(lower(haystack), lower(needle), ''))) / greatest(length(needle), 1)
$$;
