-- SearchChatHistory matches attachment file names via file.original_name
-- ILIKE '%q%'; the leading-wildcard pattern needs a trgm index to avoid a
-- full scan of the file table on every search.
-- Version: 1.1.24
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_file_original_name_trgm
    ON file USING GIN (original_name gin_trgm_ops);
