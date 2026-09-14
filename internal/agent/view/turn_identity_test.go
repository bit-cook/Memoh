package view

import (
	"fmt"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/agent/turn"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
)

// The web transcript pairs a live or optimistic turn with its settled twin by
// (turn_id, role), one to one: adoptRenderIdentity hands each on-screen render
// key to exactly one incoming turn. That only works because a history page
// never holds two turns with the same (turn_id, role) — every visible user row
// opens its own turn (persistHistoryTurn always calls CreateHistoryTurn for
// role=user; tail linking is restricted to assistant and tool rows), so a turn
// is one request and one reply.
//
// Nothing in the converter enforces that, and a persistence path that ever
// files a second visible user row under an existing turn would produce two
// assistant turns sharing a turn id. This guard fails here, in CI, rather than
// letting the client drop turns on screen with no diagnostic. A new path that
// legitimately needs multi-segment turns has to change the client's pairing
// rule and this test together.
func TestConvertMessagesToUITurnsKeepsTurnRoleIdentityUnique(t *testing.T) {
	base := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)

	for _, testCase := range []struct {
		name     string
		messages []messagepkg.Message
	}{
		{
			name: "plain round",
			messages: []messagepkg.Message{
				userMessage(t, "u1", "turn-1", "hello", base),
				assistantMessage(t, "a1", "turn-1", "hi", base.Add(time.Second)),
			},
		},
		{
			name: "reply split across several assistant rows",
			messages: []messagepkg.Message{
				userMessage(t, "u1", "turn-1", "do it", base),
				assistantMessage(t, "a1", "turn-1", "working", base.Add(time.Second)),
				assistantMessage(t, "a2", "turn-1", "still working", base.Add(2*time.Second)),
				assistantMessage(t, "a3", "turn-1", "done", base.Add(3*time.Second)),
			},
		},
		{
			// An applied steer opens its own canonical turn inside the same run
			// (SR-TURN-001), which is what keeps the pair unique per turn id.
			name: "applied steer opens its own turn",
			messages: []messagepkg.Message{
				userMessage(t, "u1", "turn-1", "ask", base),
				assistantMessage(t, "a1", "turn-1", "before steer", base.Add(time.Second)),
				userMessage(t, "u2", "turn-2", "steer me", base.Add(2*time.Second)),
				assistantMessage(t, "a2", "turn-2", "after steer", base.Add(3*time.Second)),
			},
		},
		{
			// A background-task notification becomes its own system turn, which
			// flushes the assistant turn before it. It has to carry a turn of
			// its own or the rows around it would split one turn id in two.
			name: "background task notification",
			messages: []messagepkg.Message{
				userMessage(t, "u1", "turn-1", "run it", base),
				assistantMessage(t, "a1", "turn-1", "started", base.Add(time.Second)),
				userMessage(t, "u2", "turn-2",
					"<task-notification><task-id>task-1</task-id><status>completed</status></task-notification>",
					base.Add(2*time.Second)),
				assistantMessage(t, "a2", "turn-3", "finished", base.Add(3*time.Second)),
			},
		},
		{
			name: "several consecutive rounds",
			messages: []messagepkg.Message{
				userMessage(t, "u1", "turn-1", "one", base),
				assistantMessage(t, "a1", "turn-1", "first", base.Add(time.Second)),
				userMessage(t, "u2", "turn-2", "two", base.Add(2*time.Second)),
				assistantMessage(t, "a2", "turn-2", "second", base.Add(3*time.Second)),
				userMessage(t, "u3", "turn-3", "three", base.Add(4*time.Second)),
				assistantMessage(t, "a3", "turn-3", "third", base.Add(5*time.Second)),
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			turns := ConvertMessagesToUITurns(testCase.messages)
			if len(turns) == 0 {
				t.Fatal("converter produced no turns")
			}
			seen := make(map[string]string, len(turns))
			for index, ui := range turns {
				key := fmt.Sprintf("%s\x00%s", ui.TurnID, ui.Role)
				if first, duplicate := seen[key]; duplicate {
					t.Fatalf("turn %d (%s/%s) repeats the identity already used by %s;"+
						" the web transcript pairs turns one to one on (turn_id, role)"+
						" and would drop one of them",
						index, ui.TurnID, ui.Role, first)
				}
				seen[key] = ui.ID
			}
		})
	}
}

func userMessage(t *testing.T, id, turnID, text string, at time.Time) messagepkg.Message {
	t.Helper()
	return messagepkg.Message{
		ID:             id,
		TurnID:         turnID,
		BotID:          "bot-1",
		SessionID:      "session-1",
		Role:           "user",
		DisplayContent: text,
		Content:        mustUIMessageJSON(t, turn.ModelMessage{Role: "user", Content: mustUIRawJSON(t, text)}),
		CreatedAt:      at,
	}
}

func assistantMessage(t *testing.T, id, turnID, text string, at time.Time) messagepkg.Message {
	t.Helper()
	return messagepkg.Message{
		ID:        id,
		TurnID:    turnID,
		BotID:     "bot-1",
		SessionID: "session-1",
		Role:      "assistant",
		Content:   mustUIMessageJSON(t, turn.ModelMessage{Role: "assistant", Content: mustUIRawJSON(t, text)}),
		CreatedAt: at,
	}
}
