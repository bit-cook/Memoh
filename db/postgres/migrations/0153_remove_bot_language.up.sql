-- 0153_remove_bot_language
-- Remove the unused bot reply-language setting.
ALTER TABLE bots DROP COLUMN IF EXISTS language;
