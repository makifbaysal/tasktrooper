package executor

import (
	"context"
	"fmt"

	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *ServiceSuite) TestRunEnvReachesTheProcessesOfThatRunOnly() {
	var seen [][]string
	s.local.On("Execute", mock.Anything, `{"path":"go.mod"}`).
		Run(func(args mock.Arguments) {
			seen = append(seen, registry.TaskEnvFromContext(args.Get(0).(context.Context)))
		}).
		Return(domain.ToolResult{Name: "read_file", Content: "module demo"}).Twice()
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Return(toolCallTurn("c1", "read_file", `{"path":"go.mod"}`, domain.Usage{}), nil).Once()
	s.llm.On("Chat", mock.Anything, mock.Anything).Return(finalTurn("Done."), nil).Once()
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Return(toolCallTurn("c2", "read_file", `{"path":"go.mod"}`, domain.Usage{}), nil).Once()
	s.llm.On("Chat", mock.Anything, mock.Anything).Return(finalTurn("Done."), nil).Once()
	withEnv := s.run(KindBoard)
	withEnv.MCP = nil
	withEnv.Env = map[string]string{"NODE_VERSION": "20.11.1", "GOTOOLCHAIN": "go1.22.1+auto"}
	without := s.run(KindBoard)
	without.MCP = nil
	without.RunID = "run-2"

	_, failure := s.execute(withEnv, &recordingSink{})
	s.Require().Nil(failure)
	_, failure = s.execute(without, &recordingSink{})
	s.Require().Nil(failure)

	s.Require().Len(seen, 2)
	n := len(seen[0])
	s.Require().GreaterOrEqual(n, 2)
	s.Equal([]string{"GOTOOLCHAIN=go1.22.1+auto", "NODE_VERSION=20.11.1"}, seen[0][n-2:], "the run's env comes last, so it wins")
	s.Nil(seen[1], "a run without env leaves the shell to resolve its own toolchain")
}

func (s *ServiceSuite) TestPrepareRefusesAnEnvItCannotApply() {
	tooMany := map[string]string{}
	for i := 0; i <= maxEnvEntries; i++ {
		tooMany[fmt.Sprintf("V%d", i)] = "x"
	}
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"PATH", map[string]string{"PATH": "/opt/cloud/bin"}},
		{"path in another case", map[string]string{"Path": "/opt/cloud/bin"}},
		{"not a name", map[string]string{"NODE VERSION": "20"}},
		{"starts with a digit", map[string]string{"1GO": "x"}},
		{"NUL in the value", map[string]string{"GOFLAGS": "a\x00b"}},
		{"too many", tooMany},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			run := s.run(KindBoard)
			run.Env = tt.env

			_, failure := s.svc.Prepare(context.Background(), run)

			s.Require().NotNil(failure)
			s.Equal(CodeBadRequest, failure.Code)
		})
	}
}
