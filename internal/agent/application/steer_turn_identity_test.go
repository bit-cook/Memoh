package application

import (
	"strings"
	"testing"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
)

// A claimed steer is named before its row exists, the same way admission names
// a run's request turn. The step that consumes the input must file its user row
// under that name instead of minting a second one, or history and the live
// projection describe one input under two identities.
func TestStampSteerTurnFilesTheInjectedInputUnderTheClaimedSlot(t *testing.T) {
	slot := messagepkg.TurnSlot{TurnID: "turn-steer", Position: 42}
	committer := &agentStepCommitter{queueStep: &queueStepCoordinator{
		pendingSteer:     &sessionruntime.SteerClaimRef{ItemID: "item-1"},
		pendingSteerTurn: &slot,
	}}

	inputs := []messagepkg.PersistInput{
		// The run's request turn, already named by admission.
		{Role: "user", TurnID: "turn-request", TurnPosition: int64Ptr(7)},
		// The injected steer: the one user row nobody has named.
		{Role: "user"},
		{Role: "assistant"},
		// A tool decoration appends its own user row after execution.
		{Role: "user"},
	}
	committer.stampSteerTurn(inputs)

	if inputs[0].TurnID != "turn-request" || *inputs[0].TurnPosition != 7 {
		t.Fatalf("admission's turn was overwritten: %#v", inputs[0])
	}
	if inputs[1].TurnID != slot.TurnID {
		t.Fatalf("steer input turn id = %q, want %q", inputs[1].TurnID, slot.TurnID)
	}
	if inputs[1].TurnPosition == nil || *inputs[1].TurnPosition != slot.Position {
		t.Fatalf("steer input turn position = %v, want %d", inputs[1].TurnPosition, slot.Position)
	}
	if inputs[3].TurnID != "" {
		t.Fatalf("a later synthetic user row claimed the steer's turn: %#v", inputs[3])
	}
}

// Only the step that carries the claim is stamped. Once the steer is applied the
// coordinator forgets the slot, so a later step mints its own turn as before.
func TestStampSteerTurnIsInertWithoutAClaim(t *testing.T) {
	slot := messagepkg.TurnSlot{TurnID: "turn-steer", Position: 42}
	for name, coordinator := range map[string]*queueStepCoordinator{
		"no claim": {pendingSteerTurn: &slot},
		"no slot":  {pendingSteer: &sessionruntime.SteerClaimRef{ItemID: "item-1"}},
		"no queue": nil,
		"empty slot": {
			pendingSteer:     &sessionruntime.SteerClaimRef{ItemID: "item-1"},
			pendingSteerTurn: &messagepkg.TurnSlot{TurnID: "  ", Position: 1},
		},
	} {
		t.Run(name, func(t *testing.T) {
			inputs := []messagepkg.PersistInput{{Role: "user"}}
			(&agentStepCommitter{queueStep: coordinator}).stampSteerTurn(inputs)
			if strings.TrimSpace(inputs[0].TurnID) != "" || inputs[0].TurnPosition != nil {
				t.Fatalf("input was named without a live claim: %#v", inputs[0])
			}
		})
	}
}

func int64Ptr(value int64) *int64 { return &value }
