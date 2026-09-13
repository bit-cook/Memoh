package application

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agent/runtime/native"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

// countingAccountStore records whether the owner's account profile was read, so
// the configured-override path can assert it never pays for that lookup.
type countingAccountStore struct {
	dbstore.AccountStore
	account dbstore.AccountRecord
	reads   int
}

func (s *countingAccountStore) GetByUserID(context.Context, string) (dbstore.AccountRecord, error) {
	s.reads++
	return s.account, nil
}

func newProbeModelResolver(t *testing.T, titleModelID string) (*Service, *countingAccountStore, string) {
	t.Helper()
	botID, err := db.ParseUUID("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	ownerID, err := db.ParseUUID("22222222-2222-2222-2222-222222222222")
	if err != nil {
		t.Fatal(err)
	}
	store := &countingAccountStore{account: dbstore.AccountRecord{
		ID: ownerID.String(), TitleModelID: titleModelID,
	}}
	return &Service{
		queries:        titleModelQueries{bot: sqlc.GetBotByIDRow{ID: botID, OwnerUserID: ownerID}},
		accountService: accounts.NewService(nil, store),
	}, store, botID.String()
}

func TestResolveDiscussProbeModel(t *testing.T) {
	t.Run("bot override wins and skips the account read", func(t *testing.T) {
		svc, store, botID := newProbeModelResolver(t, "title-model")

		modelID, ownerUserID, err := svc.resolveDiscussProbeModel(context.Background(), botID, "bot-model")
		if err != nil {
			t.Fatalf("resolveDiscussProbeModel() error = %v", err)
		}
		if modelID != "bot-model" {
			t.Fatalf("modelID = %q, want %q", modelID, "bot-model")
		}
		if ownerUserID == "" {
			t.Fatal("owner user id is required for credential resolution")
		}
		if store.reads != 0 {
			t.Fatalf("account profile read %d time(s); the override path must not need it", store.reads)
		}
	})

	t.Run("falls back to the owner title model", func(t *testing.T) {
		svc, store, botID := newProbeModelResolver(t, "title-model")

		modelID, _, err := svc.resolveDiscussProbeModel(context.Background(), botID, "   ")
		if err != nil {
			t.Fatalf("resolveDiscussProbeModel() error = %v", err)
		}
		if modelID != "title-model" {
			t.Fatalf("modelID = %q, want %q", modelID, "title-model")
		}
		if store.reads != 1 {
			t.Fatalf("account profile read %d time(s), want 1", store.reads)
		}
	})

	t.Run("neither configured disables the gate", func(t *testing.T) {
		svc, _, botID := newProbeModelResolver(t, "")

		modelID, _, err := svc.resolveDiscussProbeModel(context.Background(), botID, "")
		if err != nil {
			t.Fatalf("resolveDiscussProbeModel() error = %v", err)
		}
		if modelID != "" {
			t.Fatalf("modelID = %q, want empty (gate off)", modelID)
		}
	})
}

// Private conversations use the chat contract — the model's text is the reply,
// there is no message tool to require — so the gate must not run at all. Nil
// dependencies make the assertion sharp: any lookup would panic.
func TestDiscussProbeSkipsPrivateConversations(t *testing.T) {
	svc := &Service{}
	for _, conversationType := range []string{"private", "Private", "p2p", "direct", ""} {
		t.Run("type="+conversationType, func(t *testing.T) {
			got := svc.runDiscussProbe(context.Background(),
				turn.StartTurnCommand{BotID: "bot", ThreadID: "session", ConversationType: conversationType},
				ResolveRunConfigResult{DiscussProbeModelID: "some-model"})
			if got.Ran {
				t.Fatalf("gate ran for conversation type %q; private chats are ungated", conversationType)
			}
			if got.Activated {
				t.Fatalf("gate activated for conversation type %q", conversationType)
			}
		})
	}
}

func TestExtractDiscussProbeDecision(t *testing.T) {
	cases := []struct {
		name        string
		toolCalls   []sdk.ToolCall
		wantAct     string
		wantReason  string
		wantOutcome string
	}{
		{
			name: "activation",
			toolCalls: []sdk.ToolCall{{
				ToolName: "decide",
				Input:    map[string]any{"should_act": "send", "reason": "directly asked"},
			}},
			wantAct:     discussProbeActSend,
			wantReason:  "directly asked",
			wantOutcome: discussProbeOutcomeAct,
		},
		{
			name: "no action",
			toolCalls: []sdk.ToolCall{{
				ToolName: "decide",
				Input:    map[string]any{"should_act": "no_action", "reason": "chatter"},
			}},
			wantAct:     discussProbeActNoAction,
			wantReason:  "chatter",
			wantOutcome: discussProbeOutcomeNoAction,
		},
		{
			name:        "no decide call at all",
			toolCalls:   nil,
			wantOutcome: discussProbeOutcomeMissing,
		},
		{
			name: "some other tool only",
			toolCalls: []sdk.ToolCall{{
				ToolName: "send",
				Input:    map[string]any{"text": "hi"},
			}},
			wantOutcome: discussProbeOutcomeMissing,
		},
		{
			name: "unknown should_act value",
			toolCalls: []sdk.ToolCall{{
				ToolName: "decide",
				Input:    map[string]any{"should_act": "react", "reason": "just a reaction"},
			}},
			wantReason:  "just a reaction",
			wantOutcome: discussProbeOutcomeMalformed,
		},
		{
			name: "should_act missing",
			toolCalls: []sdk.ToolCall{{
				ToolName: "decide",
				Input:    map[string]any{"reason": "forgot the verdict"},
			}},
			wantReason:  "forgot the verdict",
			wantOutcome: discussProbeOutcomeMalformed,
		},
		{
			name: "input is not an object",
			toolCalls: []sdk.ToolCall{{
				ToolName: "decide",
				Input:    "send",
			}},
			wantOutcome: discussProbeOutcomeMalformed,
		},
		{
			name: "tool name casing is tolerated",
			toolCalls: []sdk.ToolCall{{
				ToolName: "Decide",
				Input:    map[string]any{"should_act": "send", "reason": "mentioned"},
			}},
			wantAct:     discussProbeActSend,
			wantReason:  "mentioned",
			wantOutcome: discussProbeOutcomeAct,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			act, reason, outcome := extractDiscussProbeDecision(tc.toolCalls)
			if act != tc.wantAct || reason != tc.wantReason || outcome != tc.wantOutcome {
				t.Fatalf("extractDiscussProbeDecision() = (%q, %q, %q), want (%q, %q, %q)",
					act, reason, outcome, tc.wantAct, tc.wantReason, tc.wantOutcome)
			}
		})
	}
}

// The gate's core invariant, asserted against the real extractor over every
// shape a judge can produce: an activation is returned if and only if the judge
// explicitly said so. A response that is absent, unparseable, or unrecognised
// must never read as permission to speak.
func TestDiscussProbeFailsClosed(t *testing.T) {
	inputs := []any{
		nil,
		"send",
		42,
		map[string]any{},
		map[string]any{"reason": "no verdict"},
		map[string]any{"should_act": ""},
		map[string]any{"should_act": "react"},
		map[string]any{"should_act": "SEND"},
		map[string]any{"should_act": "yes"},
		map[string]any{"should_act": true},
		map[string]any{"should_act": []string{"send"}},
	}
	for _, input := range inputs {
		act, _, outcome := extractDiscussProbeDecision([]sdk.ToolCall{{ToolName: "decide", Input: input}})
		if act == discussProbeActSend {
			t.Fatalf("input %#v produced an activation (outcome %q); the gate must fail closed", input, outcome)
		}
		if outcome == discussProbeOutcomeAct {
			t.Fatalf("input %#v reported outcome %q without a valid verdict", input, outcome)
		}
	}

	// The converse: a well-formed activation must still get through, otherwise
	// "fails closed" would be satisfied by a gate that is simply stuck shut.
	act, _, outcome := extractDiscussProbeDecision([]sdk.ToolCall{{
		ToolName: "decide",
		Input:    map[string]any{"should_act": "send", "reason": "asked directly"},
	}})
	if act != discussProbeActSend || outcome != discussProbeOutcomeAct {
		t.Fatalf("a valid activation was rejected: act=%q outcome=%q", act, outcome)
	}
}

func TestGenerateDiscussActivationPrompt(t *testing.T) {
	withReason := native.GenerateDiscussActivationPrompt("They asked about the deploy status.")
	if !strings.Contains(withReason, "at least one message MUST have been sent") {
		t.Fatalf("activation prompt lost its hard requirement:\n%s", withReason)
	}
	if !strings.Contains(withReason, "> They asked about the deploy status.") {
		t.Fatalf("activation prompt did not quote the evaluator reason:\n%s", withReason)
	}

	withoutReason := native.GenerateDiscussActivationPrompt("   ")
	if strings.Contains(withoutReason, "evaluator's notes") {
		t.Fatalf("blank reason must not render a notes section:\n%s", withoutReason)
	}
	if !strings.Contains(withoutReason, "at least one message MUST have been sent") {
		t.Fatalf("activation prompt lost its hard requirement without a reason:\n%s", withoutReason)
	}
	if strings.Contains(withoutReason, "{{") {
		t.Fatalf("activation prompt left an unrendered placeholder:\n%s", withoutReason)
	}
}

func TestAppendDiscussActivation(t *testing.T) {
	base := []sdk.Message{sdk.UserMessage("hello"), sdk.UserMessage("anyone around?")}

	t.Run("disabled gate leaves the turn untouched", func(t *testing.T) {
		got := appendDiscussActivation(base, discussProbeResult{})
		if len(got) != len(base) {
			t.Fatalf("appended %d message(s) for a gate that never ran", len(got)-len(base))
		}
	})

	t.Run("declined gate appends nothing", func(t *testing.T) {
		got := appendDiscussActivation(base, discussProbeResult{Ran: true, Outcome: discussProbeOutcomeNoAction})
		if len(got) != len(base) {
			t.Fatalf("appended %d message(s) for a declined wake-up", len(got)-len(base))
		}
	})

	t.Run("activation appends exactly one trailing user message", func(t *testing.T) {
		got := appendDiscussActivation(base, discussProbeResult{
			Ran: true, Activated: true, Outcome: discussProbeOutcomeAct, Reason: "they asked a question",
		})
		if len(got) != len(base)+1 {
			t.Fatalf("len = %d, want %d", len(got), len(base)+1)
		}
		last := got[len(got)-1]
		if last.Role != sdk.MessageRoleUser {
			t.Fatalf("activation landed with role %q, want user", last.Role)
		}
		// The contract has to be the final thing the model reads; anything after
		// it reintroduces the distance the tail placement exists to remove.
		text := messageText(t, last)
		if !strings.Contains(text, "at least one message MUST have been sent") {
			t.Fatalf("trailing message is not the activation contract: %q", text)
		}
		if !strings.Contains(text, "they asked a question") {
			t.Fatalf("activation dropped the evaluator reason: %q", text)
		}
	})
}

func messageText(t *testing.T, message sdk.Message) string {
	t.Helper()
	var b strings.Builder
	for _, part := range message.Content {
		if text, ok := part.(sdk.TextPart); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}
