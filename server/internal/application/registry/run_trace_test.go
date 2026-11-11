package registry_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace"
	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace/runtracetest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestExecuteRecordsEveryCallIntoTheRunTrace(t *testing.T) {
	store := runtracetest.NewMemStore()
	rec := runtrace.Start(context.Background(), store, runtrace.Meta{RunID: uuid.New(), TaskID: uuid.New(), AgentID: uuid.New(), WorkDir: t.TempDir()})
	ctx := runtrace.WithRecorder(context.Background(), rec)

	reg := registry.New()
	reg.Register(&stubTool{name: "read_file"})
	reg.Register(&stubTool{name: "run_terminal"})
	reg.Register(&stubTool{name: "web_search"})

	reg.Execute(ctx, domain.ToolCall{ID: "1", Function: domain.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go","offset":3}`}})
	reg.ExecuteWithPolicy(registry.ContextWithTraceSource(ctx, domain.TraceSourceMCP), domain.ToolCall{ID: "2", Function: domain.FunctionCall{Name: "run_terminal", Arguments: `{"command":"ls"}`}}, domain.ToolPolicy{})
	reg.Execute(ctx, domain.ToolCall{ID: "3", Function: domain.FunctionCall{Name: "no_such_tool", Arguments: `{}`}})

	require.Len(t, store.Events, 3)
	first, second, third := store.BySeq(1), store.BySeq(2), store.BySeq(3)
	require.Equal(t, domain.TraceCategoryRead, first.Category)
	require.Equal(t, domain.TraceSourceLoop, first.Source)
	require.Equal(t, "a.go", first.Target)
	require.Equal(t, domain.TraceCategoryExec, second.Category)
	require.Equal(t, domain.TraceSourceMCP, second.Source)
	require.Equal(t, domain.TraceStatusError, third.Status, "a call the registry refused is a failed call, recorded with its error")
	require.NotEmpty(t, third.Error)
}

func TestExecuteWithoutARecorderIsUnchanged(t *testing.T) {
	reg := registry.New()
	reg.Register(&stubTool{name: "read_file"})
	res := reg.Execute(context.Background(), domain.ToolCall{ID: "1", Function: domain.FunctionCall{Name: "read_file", Arguments: `{}`}})
	require.False(t, res.IsError)
}
