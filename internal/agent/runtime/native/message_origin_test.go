package native

import (
	"context"
	sdk "github.com/felinics/twilight/sdk"
	"testing"
)

func TestReadMediaOriginFollowsAdmittedPosition(t *testing.T) {
	state := &readMediaDecorationState{injections: []readMediaInjection{{afterStep: 0, messageIndex: 5}}}
	state.reconcilePreparedMessages(1, []admittedPreparedMessage{{index: 4}, {index: 5}, {index: 6}})
	indexes := InternalFeedbackIndexes(state.withMessageOrigins(context.Background(), 1))
	if len(indexes) != 1 || indexes[0] != 1 {
		t.Fatalf("feedback origins = %v; want only image at 1, excluding surrounding user inputs", indexes)
	}
	state.reconcilePreparedMessages(1, []admittedPreparedMessage{{index: 6}})
	if got := InternalFeedbackIndexes(state.withMessageOrigins(context.Background(), 1)); len(got) != 0 {
		t.Fatalf("revoked image retained origin: %v", got)
	}
}

func TestReadMediaTerminalOriginMatchesMergedMessage(t *testing.T) {
	image := sdk.Message{Role: sdk.MessageRoleUser, Content: []sdk.MessagePart{sdk.ImagePart{Image: "data:image/png;base64,AAAA"}}}
	state := &readMediaDecorationState{injections: []readMediaInjection{{afterStep: 0, message: image, admitted: true}}}
	steps := []sdk.StepResult{{Messages: []sdk.Message{sdk.AssistantMessage("looking")}}, {Messages: []sdk.Message{sdk.AssistantMessage("done")}}}
	messages, indexes := state.mergeMessagesWithOrigins(steps, nil, -1)
	if len(messages) != 3 || len(indexes) != 1 || indexes[0] != 1 || messages[1].Role != sdk.MessageRoleUser {
		t.Fatalf("messages=%v origins=%v", messages, indexes)
	}
	_, indexes = state.mergeMessagesWithOrigins(steps, nil, 1)
	if len(indexes) != 0 {
		t.Fatalf("interrupted checkpoint feedback duplicated: %v", indexes)
	}
}
