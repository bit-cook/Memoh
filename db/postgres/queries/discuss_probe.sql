-- name: CreateDiscussProbeDecision :one
INSERT INTO bot_discuss_probe_decisions (
  bot_id,
  session_id,
  requested_at_ms,
  activated,
  outcome,
  reason,
  model_id,
  input_tokens,
  output_tokens,
  cache_read_tokens,
  cache_write_tokens
) VALUES (
  sqlc.arg(bot_id),
  sqlc.arg(session_id),
  sqlc.arg(requested_at_ms),
  sqlc.arg(activated),
  sqlc.arg(outcome),
  sqlc.arg(reason),
  sqlc.narg(model_id),
  sqlc.arg(input_tokens),
  sqlc.arg(output_tokens),
  sqlc.arg(cache_read_tokens),
  sqlc.arg(cache_write_tokens)
)
RETURNING *;

-- name: ListDiscussProbeDecisions :many
SELECT *
FROM bot_discuss_probe_decisions
WHERE team_id = public.memoh_current_team_id()
  AND session_id = sqlc.arg(session_id)
ORDER BY requested_at_ms DESC, created_at DESC
LIMIT sqlc.arg(row_limit);
