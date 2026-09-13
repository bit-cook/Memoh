-- 0153_discuss_probe_gate
-- Audit trail for the discuss probe gate: the outside-judge decision that gates
-- every discuss wake-up before the primary model runs.

CREATE TABLE IF NOT EXISTS public.bot_discuss_probe_decisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id UUID NOT NULL DEFAULT public.memoh_current_team_id() REFERENCES public.teams(id) ON DELETE RESTRICT,
    bot_id UUID NOT NULL,
    session_id UUID NOT NULL,
    requested_at_ms BIGINT NOT NULL,
    -- What the gate did. Fail-closed: every outcome other than 'act' leaves this false.
    activated BOOLEAN NOT NULL,
    -- Why. 'missing'/'malformed'/'error' record the fail-closed paths so gate
    -- quality is auditable instead of silently collapsing into 'no_action'.
    outcome TEXT NOT NULL CHECK (outcome IN ('act', 'no_action', 'missing', 'malformed', 'error')),
    reason TEXT NOT NULL DEFAULT '',
    -- Historical record of which model judged. Deliberately not a foreign key:
    -- deleting a model must not rewrite or cascade away past decisions.
    model_id UUID,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT bot_discuss_probe_decisions_bot_fkey FOREIGN KEY (team_id, bot_id)
        REFERENCES public.bots(team_id, id) ON DELETE CASCADE,
    CONSTRAINT bot_discuss_probe_decisions_session_fkey FOREIGN KEY (team_id, session_id)
        REFERENCES public.bot_sessions(team_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_bot_discuss_probe_decisions_session_recent
    ON public.bot_discuss_probe_decisions (team_id, session_id, requested_at_ms DESC);

ALTER TABLE public.bot_discuss_probe_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.bot_discuss_probe_decisions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS bot_discuss_probe_decisions_team ON public.bot_discuss_probe_decisions;
CREATE POLICY bot_discuss_probe_decisions_team ON public.bot_discuss_probe_decisions
    USING (team_id = public.memoh_current_team_id())
    WITH CHECK (team_id = public.memoh_current_team_id());
