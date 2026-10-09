package mcp

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type RemoteConnectorSuite struct {
	suite.Suite
	board  *mocks.ToolExecutor
	tokens *mcpserver.RunTokenRegistry
	server *httptest.Server
	token  string
}

func TestRemoteConnectorSuite(t *testing.T) {
	suite.Run(t, new(RemoteConnectorSuite))
}

func (s *RemoteConnectorSuite) tool(name string) *mocks.ToolExecutor {
	tool := mocks.NewToolExecutor(s.T())
	tool.On("Name").Return(name).Maybe()
	tool.On("Definition").Return(domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: name, Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}).Maybe()
	return tool
}

// The cloud's own endpoint, not a stand-in: what the executor must speak is
// exactly what adapter/mcpserver serves.
func (s *RemoteConnectorSuite) SetupTest() {
	reg := registry.New()
	s.board = s.tool("list_board_tasks")
	reg.Register(s.board)
	reg.Register(s.tool("read_file"))
	s.tokens = mcpserver.NewRunTokenRegistry()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	mcpserver.New(reg, s.tokens).Register(app)
	s.server = httptest.NewServer(adaptor.FiberApp(app))
	var err error
	s.token, err = s.tokens.Mint(mcpserver.Run{
		Ctx:    context.Background(),
		Policy: domain.ToolPolicy{AllowTools: []string{"list_board_tasks", "read_file"}},
	})
	s.Require().NoError(err)
}

func (s *RemoteConnectorSuite) TearDownTest() {
	s.server.Close()
}

func (s *RemoteConnectorSuite) endpoint() port.RemoteToolEndpoint {
	return port.RemoteToolEndpoint{URL: s.server.URL + mcpserver.Path, Token: s.token, ServerName: "tasktrooper"}
}

func (s *RemoteConnectorSuite) TestToolsArriveUnderTheirServedNamesAndRunRemotely() {
	s.board.On("Execute", mock.Anything, `{"status":"todo"}`).
		Return(domain.ToolResult{Name: "list_board_tasks", Content: `[{"key":"T-1"}]`}).Once()

	tools, closeSession, err := RemoteConnector{}.Connect(context.Background(), s.endpoint())
	s.Require().NoError(err)
	defer closeSession()

	s.Require().Len(tools, 1)
	s.Equal("list_board_tasks", tools[0].Name())
	s.Equal("list_board_tasks", tools[0].Definition().Function.Name)
	_, namespacedByServer := tools[0].(port.MCPServerTool)
	s.False(namespacedByServer)

	result := tools[0].Execute(context.Background(), `{"status":"todo"}`)
	s.False(result.IsError)
	s.Equal(`[{"key":"T-1"}]`, result.Content)
}

func (s *RemoteConnectorSuite) TestARevokedTokenIsRefused() {
	s.tokens.Revoke(s.token)

	_, _, err := RemoteConnector{}.Connect(context.Background(), s.endpoint())

	s.Error(err)
}

func (s *RemoteConnectorSuite) TestANonHTTPEndpointIsRefused() {
	endpoint := s.endpoint()
	endpoint.URL = "file:///etc/passwd"

	_, _, err := RemoteConnector{}.Connect(context.Background(), endpoint)

	s.Error(err)
}
