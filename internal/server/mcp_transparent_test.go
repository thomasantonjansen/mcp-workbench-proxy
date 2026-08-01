package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
)

func TestBuildTransparentDirectToolChangesOnlyName(t *testing.T) {
	raw := `{
		"name":"inspect","title":"Inspect","description":"Exact description",
		"inputSchema":{"type":"object","properties":{"target":{"oneOf":[{"type":"string"},{"type":"integer"}]}},"allOf":[{"required":["target"]}],"additionalProperties":false},
		"outputSchema":{"type":"object","properties":{"ok":{"type":"boolean"}},"unevaluatedProperties":false},
		"annotations":{"title":"Inspect exact","readOnlyHint":true},
		"_meta":{"vendor/value":"kept"},"defer_loading":true,
		"icons":[{"src":"data:image/png;base64,AA==","mimeType":"image/png"}],
		"execution":{"taskSupport":"optional"}
	}`
	tool := buildTransparentDirectTool(&config.ToolMetadata{
		Name: "inspect", ServerName: "unity", RawToolJSON: raw,
	}, "unity__inspect")

	encoded, err := json.Marshal(tool)
	require.NoError(t, err)
	var want, got map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(raw), &want))
	require.NoError(t, json.Unmarshal(encoded, &got))
	want["name"] = "unity__inspect"
	require.Equal(t, want, got)
}

func TestVendoredMCPSeamPreservesProtocolErrorDetails(t *testing.T) {
	server := mcpserver.NewMCPServer("upstream", "1", mcpserver.WithToolCapabilities(false))
	server.AddTool(mcp.NewTool("explode"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, &mcp.JSONRPCErrorDetailsError{Details: mcp.JSONRPCErrorDetails{
			Code: -32099, Message: "upstream exploded", Data: map[string]interface{}{"detail": "exact"},
		}}
	})
	client, err := mcpclient.NewInProcessClient(server)
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))
	t.Cleanup(func() { _ = client.Close() })
	initialize := mcp.InitializeRequest{}
	initialize.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initialize.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	_, err = client.Initialize(t.Context(), initialize)
	require.NoError(t, err)
	_, err = client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "explode"}})
	var protocolErr *mcp.JSONRPCErrorDetailsError
	require.True(t, errors.As(err, &protocolErr))
	require.Equal(t, -32099, protocolErr.Details.Code)
	require.Equal(t, "upstream exploded", protocolErr.Details.Message)
	require.Equal(t, map[string]interface{}{"detail": "exact"}, protocolErr.Details.Data)
}
