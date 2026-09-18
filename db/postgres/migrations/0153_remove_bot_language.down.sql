-- 0153_remove_bot_language
-- Restore the retired column with its default; discarded values cannot be recovered.
ALTER TABLE bots ADD COLUMN IF NOT EXISTS language TEXT NOT NULL DEFAULT 'auto';
