-- 0153_discuss_probe_gate (rollback)
-- Remove the discuss probe gate audit trail.

DROP TABLE IF EXISTS public.bot_discuss_probe_decisions;
