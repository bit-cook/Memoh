package application

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/agent/runtime/native"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/oauthctx"
	"github.com/felinics/memoh/internal/providers"
)

const (
	discussProbeTimeout = 45 * time.Second
	// discussProbeMaxTokens caps the probe completion. Same two-sided bound as
	// title generation: reasoning models burn hidden thinking before emitting
	// the tool call, so a small cap truncates the decision away, while a large
	// cap makes short-context models reject the request outright.
	discussProbeMaxTokens = 2048
	// discussProbeContextMaxTokens bounds the judge's view independently of the
	// primary budget. Admission keeps compaction summaries plus the newest
	// messages, which is exactly the surface needed to judge "should the bot
	// speak right now" — and keeps the gate from re-walking the same cliff the
	// primary path OOM'd on.
	discussProbeContextMaxTokens = 16384

	discussProbeToolName = "decide"

	// should_act values. Deliberately two-valued: there is no "react only"
	// option, because a standalone reaction must not wake the primary.
	discussProbeActSend     = "send"
	discussProbeActNoAction = "no_action"

	// Outcomes recorded for audit. Everything other than "act" leaves the gate
	// closed; the distinct values exist so a silent bot can be diagnosed as a
	// judgement, a parse failure, or an outage.
	discussProbeOutcomeAct       = "act"
	discussProbeOutcomeNoAction  = "no_action"
	discussProbeOutcomeMissing   = "missing"
	discussProbeOutcomeMalformed = "malformed"
	discussProbeOutcomeError     = "error"
)

// discussProbeResult is the gate's verdict for one discuss wake-up.
type discussProbeResult struct {
	// Ran reports whether a probe model was configured and actually judged.
	// When false the gate is disabled and the caller proceeds unchanged.
	Ran       bool
	Activated bool
	Reason    string
	Outcome   string
}

// resolveDiscussProbeModel picks the model that judges discuss wake-ups:
// the bot's own probe model when set, otherwise the owner's title model.
//
// The fallback is deliberate rather than incidental. Both slots want the same
// kind of model — cheap, fast, no tools of its own — so an operator who already
// chose one for titles gets a working gate without a second decision. The
// bot-level setting still wins, because group-chat policy is per-bot while the
// title model is an account-wide preference.
//
// botConfigured comes from the settings the run-config builder already read, so
// the common path costs one bot row for the owner and nothing else. Only the
// fallback pays for the owner's account profile.
//
// An empty model ID is not an error: it means the gate is disabled.
func (s *Service) resolveDiscussProbeModel(ctx context.Context, botID, botConfigured string) (modelID, ownerUserID string, err error) {
	if configured := strings.TrimSpace(botConfigured); configured != "" {
		ownerUserID, err = s.resolveBotOwnerUserID(ctx, botID)
		if err != nil {
			return "", "", err
		}
		return configured, ownerUserID, nil
	}
	return s.resolveTitleModel(ctx, botID)
}

// discussProbeTool is the probe's only move. It carries no side effect: the
// runner never executes it, it is read back out of the response.
func discussProbeTool() sdk.Tool {
	return sdk.Tool{
		Name: discussProbeToolName,
		Description: "Report your judgement on whether the bot should act in this conversation right now. " +
			"This is your only output.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"should_act": map[string]any{
					"type": "string",
					"enum": []string{discussProbeActSend, discussProbeActNoAction},
					"description": "\"" + discussProbeActSend + "\" when the bot should act this wake-up and that action must include at least one message; " +
						"\"" + discussProbeActNoAction + "\" when the bot should stay silent and its wake-up should not run.",
				},
				"reason": map[string]any{
					"type": "string",
					"description": "Brief justification. When should_act is \"" + discussProbeActSend +
						"\" this is forwarded to the bot as advisory context for what to say and what to do first.",
				},
			},
			"required": []string{"should_act", "reason"},
		},
	}
}

// extractDiscussProbeDecision reads the decide call out of a probe response.
// A missing or unparseable decision is reported as such rather than coerced,
// so the caller can fail closed and record which failure it was.
func extractDiscussProbeDecision(toolCalls []sdk.ToolCall) (shouldAct, reason, outcome string) {
	for _, call := range toolCalls {
		if !strings.EqualFold(strings.TrimSpace(call.ToolName), discussProbeToolName) {
			continue
		}
		raw, err := json.Marshal(call.Input)
		if err != nil {
			return "", "", discussProbeOutcomeMalformed
		}
		var decoded struct {
			ShouldAct string `json:"should_act"`
			Reason    string `json:"reason"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return "", "", discussProbeOutcomeMalformed
		}
		switch strings.TrimSpace(decoded.ShouldAct) {
		case discussProbeActSend:
			return discussProbeActSend, strings.TrimSpace(decoded.Reason), discussProbeOutcomeAct
		case discussProbeActNoAction:
			return discussProbeActNoAction, strings.TrimSpace(decoded.Reason), discussProbeOutcomeNoAction
		default:
			return "", strings.TrimSpace(decoded.Reason), discussProbeOutcomeMalformed
		}
	}
	return "", "", discussProbeOutcomeMissing
}

// runDiscussProbe judges whether this discuss wake-up should wake the primary.
//
// Fail-closed is the whole point of the gate: a missing model, an unreachable
// provider, an overflowing context, or a decision the judge never emitted all
// resolve to "do not act". The one exception is a gate that was never
// configured, which returns Ran=false and leaves the caller's behavior
// untouched.
func (s *Service) runDiscussProbe(ctx context.Context, cmd turn.StartTurnCommand, resolved ResolveRunConfigResult) discussProbeResult {
	// Private conversations are a different contract and are deliberately not
	// gated. Chat mode delivers the model's text straight to the user, so there
	// is no message tool to require and no "should I interject?" question to
	// judge — every message in a DM is addressed to the bot by construction.
	// The gate exists for group chatter, where the bot is an observer.
	//
	// Note that an unset conversation type normalizes to private, so it also
	// bypasses. That is the repo-wide convention (the same predicate decides
	// `DiscussAddressed`), and erring toward the ungated contract keeps an
	// unclassified conversation answerable rather than silently mute.
	if turn.IsPrivateConversationType(cmd.ConversationType) {
		return discussProbeResult{}
	}

	probeModelID, ownerUserID, err := s.resolveDiscussProbeModel(ctx, cmd.BotID, resolved.DiscussProbeModelID)
	if err != nil {
		s.logger.Warn("discuss probe: failed to resolve probe model",
			slog.String("bot_id", cmd.BotID),
			slog.Any("error", err))
		return discussProbeResult{}
	}
	if probeModelID == "" {
		s.logger.Debug("discuss probe: no probe model configured, gate disabled",
			slog.String("bot_id", cmd.BotID))
		return discussProbeResult{}
	}

	requestedAtMs := time.Now().UnixMilli()
	result := discussProbeResult{Ran: true, Outcome: discussProbeOutcomeError}

	probeModel, provider, err := s.fetchChatModel(ctx, probeModelID)
	if err != nil {
		s.logger.Warn("discuss probe: failed to resolve model",
			slog.String("model_id", probeModelID),
			slog.Any("error", err))
		s.persistDiscussProbeDecision(ctx, cmd, requestedAtMs, result, "", sdk.Usage{})
		return result
	}

	admitted, admission := admitDiscussMessages(cmd.DiscussMessages, discussProbeContextMaxTokens)
	if admission.ProtectedOverflow {
		s.logger.Warn("discuss probe: context overflow, failing closed",
			slog.String("bot_id", cmd.BotID),
			slog.String("session_id", cmd.ThreadID),
			slog.Int("estimated_tokens", admission.EstimatedTokens),
			slog.Int("budget_tokens", admission.BudgetTokens))
		s.persistDiscussProbeDecision(ctx, cmd, requestedAtMs, result, probeModelID, sdk.Usage{})
		return result
	}
	messages := discussMessagesToSDK(admitted)
	if len(messages) == 0 {
		s.logger.Debug("discuss probe: empty context, failing closed",
			slog.String("session_id", cmd.ThreadID))
		s.persistDiscussProbeDecision(ctx, cmd, requestedAtMs, result, probeModelID, sdk.Usage{})
		return result
	}

	// The run config already carries the bot identity; re-reading the row here
	// would be a second query for a value we were handed.
	botInfo := resolved.RunConfig.Bot
	system := native.GenerateDiscussProbePrompt(botInfo, botInfo.Timezone)

	authService := providers.NewService(nil, s.queries, "")
	authCtx := oauthctx.WithUserID(ctx, ownerUserID)
	creds, err := authService.ResolveModelCredentials(authCtx, provider)
	if err != nil {
		s.logger.Warn("discuss probe: failed to resolve provider credentials", slog.Any("error", err))
		s.persistDiscussProbeDecision(ctx, cmd, requestedAtMs, result, probeModelID, sdk.Usage{})
		return result
	}

	sdkModel := models.NewSDKChatModel(models.SDKModelConfig{
		ModelID:               probeModel.ModelID,
		ClientType:            provider.ClientType,
		APIKey:                creds.APIKey,
		CodexAccountID:        creds.CodexAccountID,
		BaseURL:               providers.ProviderConfigString(provider, "base_url"),
		ChatCompletionsCompat: providers.ProviderConfigString(provider, models.ChatCompletionsCompatConfigKey),
	})

	probeCtx, cancel := context.WithTimeout(ctx, discussProbeTimeout)
	defer cancel()

	tools := []sdk.Tool{discussProbeTool()}
	cacheTTL := providers.ProviderConfigString(provider, "prompt_cache_ttl")
	cachedSystem, cachedMessages, cachedTools := models.ApplyPromptCache(sdkModel, cacheTTL, system, messages, tools)

	// MaxSteps stays at its zero default: one call, no tool auto-execution.
	// `decide` reports a judgement; there is nothing to execute.
	generated, err := sdk.NewClient().GenerateTextResult(probeCtx,
		sdk.WithModel(sdkModel),
		sdk.WithSystem(cachedSystem),
		sdk.WithMessages(cachedMessages),
		sdk.WithTools(cachedTools),
		sdk.WithToolChoice(map[string]any{
			"type":     "function",
			"function": map[string]any{"name": discussProbeToolName},
		}),
		sdk.WithMaxTokens(discussProbeMaxTokens),
	)
	if err != nil {
		s.logger.Warn("discuss probe: model call failed, failing closed",
			slog.String("session_id", cmd.ThreadID),
			slog.Any("error", err))
		s.persistDiscussProbeDecision(ctx, cmd, requestedAtMs, result, probeModelID, sdk.Usage{})
		return result
	}

	shouldAct, reason, outcome := extractDiscussProbeDecision(generated.ToolCalls)
	result.Outcome = outcome
	result.Reason = reason
	result.Activated = shouldAct == discussProbeActSend

	s.logger.Info("discuss probe: decision",
		slog.String("bot_id", cmd.BotID),
		slog.String("session_id", cmd.ThreadID),
		slog.String("model_id", probeModelID),
		slog.Bool("activated", result.Activated),
		slog.String("outcome", result.Outcome),
		slog.String("reason", result.Reason))

	s.persistDiscussProbeDecision(ctx, cmd, requestedAtMs, result, probeModelID, generated.Usage)
	return result
}

// appendDiscussActivation carries the probe's activation contract into the
// primary turn as the last user message.
//
// Tail placement is the point. The system prompt is where a standing rule would
// normally live, but this one has to survive a long history: a requirement
// stated once, thousands of tokens back, is exactly what a model drops. Sitting
// adjacent to the generation point, it cannot be crowded out.
//
// A gate that did not run, or ran and declined, appends nothing — the caller
// never reaches here on a decline, and a disabled gate must leave the turn
// byte-identical to what it was before this feature existed.
func appendDiscussActivation(messages []sdk.Message, probe discussProbeResult) []sdk.Message {
	if !probe.Ran || !probe.Activated {
		return messages
	}
	return append(messages, sdk.UserMessage(native.GenerateDiscussActivationPrompt(probe.Reason)))
}

// persistDiscussProbeDecision records the verdict. Persistence failures are
// logged and swallowed: an audit-trail outage must not decide whether the bot
// speaks.
func (s *Service) persistDiscussProbeDecision(
	ctx context.Context,
	cmd turn.StartTurnCommand,
	requestedAtMs int64,
	result discussProbeResult,
	modelID string,
	usage sdk.Usage,
) {
	if s.queries == nil {
		return
	}
	botUUID, err := db.ParseUUID(cmd.BotID)
	if err != nil {
		return
	}
	sessionUUID, err := db.ParseUUID(cmd.ThreadID)
	if err != nil {
		return
	}
	modelUUID := pgtype.UUID{}
	if parsed, parseErr := db.ParseUUID(modelID); parseErr == nil {
		modelUUID = parsed
	}
	if _, err := s.queries.CreateDiscussProbeDecision(ctx, sqlc.CreateDiscussProbeDecisionParams{
		BotID:            botUUID,
		SessionID:        sessionUUID,
		RequestedAtMs:    requestedAtMs,
		Activated:        result.Activated,
		Outcome:          result.Outcome,
		Reason:           result.Reason,
		ModelID:          modelUUID,
		InputTokens:      int32(usage.InputTokens),       //nolint:gosec // provider-reported counts are well below int32
		OutputTokens:     int32(usage.OutputTokens),      //nolint:gosec // provider-reported counts are well below int32
		CacheReadTokens:  int32(usage.CachedInputTokens), //nolint:gosec // provider-reported counts are well below int32
		CacheWriteTokens: 0,
	}); err != nil {
		s.logger.Warn("discuss probe: failed to persist decision",
			slog.String("session_id", cmd.ThreadID),
			slog.Any("error", err))
	}
}
