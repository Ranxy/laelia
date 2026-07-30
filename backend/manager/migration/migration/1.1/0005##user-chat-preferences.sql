-- Per-user chat composer preferences (proto/store/store/user.proto ChatPreferences).
-- Nullable on purpose: a NULL value means "use the default" (enter_to_send =
-- true, the historic Enter-sends behavior); only an explicit user write stores
-- a real object. The application reads NULL as the default on the way out.
ALTER TABLE principal ADD COLUMN IF NOT EXISTS chat_preferences jsonb;