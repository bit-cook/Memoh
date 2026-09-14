package sessionruntime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	chatview "github.com/felinics/memoh/internal/agent/view"
)

// A live run must publish the turn position admission drew for it. Without it a
// subscriber can only order a running turn against settled history by its
// timestamp, which SR-TURN-001 rules out.
func TestAdmitPublishesTurnPositionOnTheRunView(t *testing.T) {
	fixture := newAdmitFixture(t)
	in := fixture.input("invocation-position", "payload")
	in.Execution.Admission = func(_ context.Context, handle RunHandle) (RunAdmissionView, error) {
		return RunAdmissionView{RequestUserTurn: &chatview.UITurn{
			TurnID: handle.TurnID, Role: "user", Text: "hello", Timestamp: time.Now(),
		}}, nil
	}

	admission, err := fixture.manager.Admit(context.Background(), in)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if admission.TurnPosition <= 0 {
		t.Fatalf("admission turn position = %d, want a positive slot", admission.TurnPosition)
	}

	snapshot, err := fixture.manager.Snapshot(context.Background(), testBotID, testSessionID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	run := snapshot.CurrentRunView
	if run == nil {
		t.Fatal("current run view is missing")
	}
	if run.TurnPosition != admission.TurnPosition {
		t.Fatalf("run view turn position = %d, want %d", run.TurnPosition, admission.TurnPosition)
	}
	if len(run.UserTurns) != 1 {
		t.Fatalf("user turns = %#v, want the request turn", run.UserTurns)
	}
	position := run.UserTurns[0].TurnPosition
	if position == nil || *position != admission.TurnPosition {
		t.Fatalf("request user turn position = %v, want %d", position, admission.TurnPosition)
	}
}

// A steer opens its own canonical turn, and history numbers it at commit. The
// runtime must not renumber it with the run's own slot.
func TestPublishQueueUserTurnsKeepsHistoryNumbering(t *testing.T) {
	fixture := newAdmitFixture(t)
	in := fixture.input("invocation-steer-position", "payload")
	in.Execution.Admission = func(_ context.Context, handle RunHandle) (RunAdmissionView, error) {
		return RunAdmissionView{RequestUserTurn: &chatview.UITurn{
			TurnID: handle.TurnID, Role: "user", Text: "hello", Timestamp: time.Now(),
		}}, nil
	}
	admission, err := fixture.manager.Admit(context.Background(), in)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}

	steerPosition := admission.TurnPosition + 7
	steer := chatview.UITurn{
		TurnID: "turn-steer", TurnPosition: &steerPosition, Role: "user",
		Text: "steer me", ID: "persisted-steer", Timestamp: time.Now(),
	}
	if err := fixture.manager.PublishQueueUserTurns(context.Background(), admission.Handle, QueueUserTurnUpdate{
		PersistedTurns: []chatview.UITurn{steer},
	}); err != nil {
		t.Fatalf("publish steer turn: %v", err)
	}

	snapshot, err := fixture.manager.Snapshot(context.Background(), testBotID, testSessionID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	turns := snapshot.CurrentRunView.UserTurns
	if len(turns) != 2 {
		t.Fatalf("user turns = %#v, want request plus steer", turns)
	}
	if turns[1].TurnPosition == nil || *turns[1].TurnPosition != steerPosition {
		t.Fatalf("steer turn position = %v, want %d", turns[1].TurnPosition, steerPosition)
	}
	if turns[0].TurnPosition == nil || *turns[0].TurnPosition != admission.TurnPosition {
		t.Fatalf("request turn position = %v, want %d", turns[0].TurnPosition, admission.TurnPosition)
	}
}

// The wire view is the only thing a subscriber sees, so the field has to
// survive the codec both ways.
func TestRunViewWireCarriesTurnPosition(t *testing.T) {
	snapshot := Snapshot{
		BotID: testBotID, SessionID: testSessionID, Epoch: "e", Seq: 3,
		CurrentRunView: &CurrentRunView{
			RunID: testRunID, TurnID: "turn-1", TurnPosition: 42,
			Status: RunStatusRunning, Messages: []chatview.UIMessage{},
		},
	}
	encoded, err := marshalSnapshot(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	var wire struct {
		CurrentRunView struct {
			TurnPosition int64 `json:"turn_position"`
		} `json:"current_run_view"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("decode wire snapshot: %v", err)
	}
	if wire.CurrentRunView.TurnPosition != 42 {
		t.Fatalf("wire turn_position = %d, want 42", wire.CurrentRunView.TurnPosition)
	}

	var decoded Snapshot
	if err := unmarshalSnapshot(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if decoded.CurrentRunView == nil || decoded.CurrentRunView.TurnPosition != 42 {
		t.Fatalf("decoded turn position = %#v, want 42", decoded.CurrentRunView)
	}
}
