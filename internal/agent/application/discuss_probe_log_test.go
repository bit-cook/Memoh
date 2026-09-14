package application

import (
	"context"
	"log/slog"
	"testing"

	"github.com/felinics/memoh/internal/agent/turn"
)

// capturingHandler keeps the structured fields of each record so a test can
// assert on the account the gate leaves behind.
type capturingHandler struct {
	records []map[string]any
}

func (*capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, record slog.Record) error {
	fields := map[string]any{"msg": record.Message, "level": record.Level.String()}
	record.Attrs(func(attr slog.Attr) bool {
		fields[attr.Key] = attr.Value.Any()
		return true
	})
	h.records = append(h.records, fields)
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

func runProbeCapturingLogs(t *testing.T, cmd turn.StartTurnCommand, resolved ResolveRunConfigResult) (discussProbeResult, []map[string]any) {
	t.Helper()
	handler := &capturingHandler{}
	svc := &Service{logger: slog.New(handler)}
	return svc.runDiscussProbe(context.Background(), cmd, resolved), handler.records
}

func groupCommand() turn.StartTurnCommand {
	return turn.StartTurnCommand{BotID: "bot-1", ThreadID: "sess-1", ConversationType: "group"}
}

// The gate keeps no table; this log is the entire account of why a bot did or
// did not speak. The question it must answer — judgement, broken config, or
// outage? — is only answerable if every exit emits the same field set with a
// distinguishing cause.
func TestDiscussProbeLogsEveryExitUniformly(t *testing.T) {
	requiredFields := []string{"bot_id", "session_id", "model_id", "cause", "outcome", "gate_ran", "activated"}

	cases := []struct {
		name        string
		resolved    ResolveRunConfigResult
		wantCause   string
		wantGateRan bool
		wantOutcome string
	}{
		{
			// A configured gate that cannot read its config holds the turn shut.
			name:        "configured gate with unreadable config",
			resolved:    ResolveRunConfigResult{DiscussProbeModelID: "configured-model"},
			wantCause:   "config_unreadable",
			wantGateRan: true,
			wantOutcome: discussProbeOutcomeError,
		},
		{
			// No override: nothing was bypassed, the chat simply has no gate.
			name:        "unconfigured gate with failed fallback lookup",
			resolved:    ResolveRunConfigResult{},
			wantCause:   "fallback_lookup_failed",
			wantGateRan: false,
			wantOutcome: "disabled",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, records := runProbeCapturingLogs(t, groupCommand(), tc.resolved)
			if len(records) != 1 {
				t.Fatalf("emitted %d records, want exactly 1 per gate exit", len(records))
			}
			record := records[0]
			for _, field := range requiredFields {
				if _, ok := record[field]; !ok {
					t.Fatalf("record is missing %q; a varying shape makes the log unqueryable: %v", field, record)
				}
			}
			if record["cause"] != tc.wantCause {
				t.Fatalf("cause = %v, want %q", record["cause"], tc.wantCause)
			}
			if record["gate_ran"] != tc.wantGateRan {
				t.Fatalf("gate_ran = %v, want %v", record["gate_ran"], tc.wantGateRan)
			}
			if record["outcome"] != tc.wantOutcome {
				t.Fatalf("outcome = %v, want %q", record["outcome"], tc.wantOutcome)
			}
			if result.Ran != tc.wantGateRan {
				t.Fatalf("verdict Ran = %v but log says %v; the record must describe the decision returned", result.Ran, tc.wantGateRan)
			}
		})
	}
}

// A silent bot is diagnosable only if "the gate held it shut" and "there was no
// gate" are distinguishable. They are the two cases an operator confuses.
func TestDiscussProbeLogDistinguishesShutFromAbsent(t *testing.T) {
	_, shut := runProbeCapturingLogs(t, groupCommand(), ResolveRunConfigResult{DiscussProbeModelID: "configured-model"})
	_, absent := runProbeCapturingLogs(t, groupCommand(), ResolveRunConfigResult{})

	if shut[0]["cause"] == absent[0]["cause"] {
		t.Fatalf("both exits report cause=%v; the two are indistinguishable in the log", shut[0]["cause"])
	}
	if shut[0]["level"] != "WARN" {
		t.Fatalf("a gate holding a turn shut logged at %v; it must be visible without debug logging", shut[0]["level"])
	}
}

// Private chats are ungated, so the gate must not narrate them at all — a line
// per direct message would drown the records that matter.
func TestDiscussProbeLogsNothingForPrivateChats(t *testing.T) {
	result, records := runProbeCapturingLogs(t,
		turn.StartTurnCommand{BotID: "bot-1", ThreadID: "sess-1", ConversationType: "private"},
		ResolveRunConfigResult{DiscussProbeModelID: "configured-model"})

	if len(records) != 0 {
		t.Fatalf("emitted %d records for a private chat: %v", len(records), records)
	}
	if result.Ran {
		t.Fatal("gate ran for a private chat")
	}
}
