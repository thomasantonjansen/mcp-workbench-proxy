package server

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestScopedCatalogSeparatesAgentVisibilityFromController(t *testing.T) {
	publicationTool, err := json.Marshal(mcp.NewTool("umc_action_1_check"))
	require.NoError(t, err)
	proxy := &MCPProxyServer{logger: zap.NewNop()}
	serverCfg := &config.ServerConfig{
		ID: "server-uuid", Name: "unityMCP",
		AgentHiddenTools: []string{"hidden_tool"},
		Publications: []config.PublishedToolConfig{{
			PublicationID: "action:1", Name: "umc_action_1_check",
			Kind: "action", Tool: publicationTool,
		}},
	}
	tools := []*config.ToolMetadata{
		{Name: "visible_tool", ServerName: "unityMCP", RawToolJSON: `{"name":"visible_tool","inputSchema":{"type":"object"}}`},
		{Name: "hidden_tool", ServerName: "unityMCP", RawToolJSON: `{"name":"hidden_tool","inputSchema":{"type":"object"}}`},
	}

	agent, controller := proxy.buildScopedTools(serverCfg, tools)
	require.Len(t, controller, 2)
	require.Len(t, agent, 2)
	require.Equal(t, []string{"hidden_tool", "visible_tool"}, []string{
		controller[0].Tool.Name, controller[1].Tool.Name,
	})
	require.Equal(t, []string{"visible_tool", "umc_action_1_check"}, []string{
		agent[0].Tool.Name, agent[1].Tool.Name,
	})
}

func TestStableServerIDPrefersImmutableUUID(t *testing.T) {
	require.Equal(t, "uuid", stableServerID(&config.ServerConfig{ID: "uuid", Name: "renamed"}))
	require.Equal(t, "legacy", stableServerID(&config.ServerConfig{Name: "legacy"}))
}
