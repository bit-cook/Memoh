package schedule

import (
	"context"
	"testing"

	"github.com/robfig/cron/v3"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

type createQueries struct {
	executionQueries
	created *sqlc.CreateScheduleParams
}

func (q *createQueries) CreateSchedule(_ context.Context, params sqlc.CreateScheduleParams) (sqlc.Schedule, error) {
	q.created = &params
	return sqlc.Schedule{Name: params.Name, Description: params.Description, Command: params.Command, Pattern: params.Pattern}, nil
}

func TestCreateWithoutDescription(t *testing.T) {
	queries := &createQueries{}
	svc := newExecutionService(t, &queries.executionQueries, nil)
	svc.queries = queries
	svc.parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	disabled := false
	result, err := svc.Create(context.Background(), execTestBotID, CreateRequest{
		Name: "Daily report", Pattern: "0 9 * * *", Command: "Summarize today's work", Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("create without description: %v", err)
	}
	if queries.created == nil || queries.created.Description != "" || result.Description != "" {
		t.Fatalf("empty description was not preserved: %+v", result)
	}
}
