package server

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
)

func TestLocalCallLogWritesCompleteConcurrentJSONLLines(t *testing.T) {
	dir := t.TempDir()
	proxy := &MCPProxyServer{
		logger: zap.NewNop(),
		config: &config.Config{
			DataDir: dir,
			Logging: &config.LogConfig{Enabled: true, CallsFilename: "calls.jsonl"},
		},
	}
	result := &mcp.CallToolResult{
		Content:           []mcp.Content{mcp.TextContent{Type: "text", Text: "complete"}},
		StructuredContent: map[string]interface{}{"ok": true},
	}

	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			proxy.writeLocalCallLog(
				context.Background(), time.Now(), "session", "unity__inspect",
				"unity", "inspect", map[string]interface{}{"secret": "kept"}, result, nil,
			)
		}()
	}
	group.Wait()

	file, err := os.Open(filepath.Join(dir, "logs", "calls.jsonl"))
	require.NoError(t, err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		var record map[string]interface{}
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &record))
		require.Equal(t, "unity__inspect", record["public_tool_name"])
		require.Equal(t, "unity", record["source_server"])
		require.Equal(t, "inspect", record["original_tool_name"])
		require.Equal(t, "kept", record["input"].(map[string]interface{})["secret"])
		require.NotNil(t, record["output"])
		count++
	}
	require.NoError(t, scanner.Err())
	require.Equal(t, 20, count)
}

func TestLocalCallLogDisabledCreatesNoFile(t *testing.T) {
	dir := t.TempDir()
	proxy := &MCPProxyServer{
		logger: zap.NewNop(),
		config: &config.Config{DataDir: dir, Logging: &config.LogConfig{Enabled: false}},
	}
	proxy.writeLocalCallLog(context.Background(), time.Now(), "", "a__b", "a", "b", map[string]interface{}{}, nil, nil)
	_, err := os.Stat(filepath.Join(dir, "logs", "calls.jsonl"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
