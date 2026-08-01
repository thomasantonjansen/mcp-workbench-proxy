package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

type localCallLogRecord struct {
	Timestamp      string      `json:"timestamp"`
	StartedAt      string      `json:"started_at"`
	DurationMS     int64       `json:"duration_ms"`
	Client         string      `json:"client_name,omitempty"`
	ClientVersion  string      `json:"client_version,omitempty"`
	SessionID      string      `json:"session_id,omitempty"`
	PublicToolName string      `json:"public_tool_name"`
	ServerID       string      `json:"server_id"`
	SourceServer   string      `json:"source_server"`
	OriginalTool   string      `json:"original_tool_name"`
	ToolKind       string      `json:"tool_kind,omitempty"`
	PublicationID  string      `json:"publication_id,omitempty"`
	Caller         string      `json:"caller,omitempty"`
	Input          interface{} `json:"input"`
	Output         interface{} `json:"output,omitempty"`
	Error          interface{} `json:"error,omitempty"`
}

// writeLocalCallLog writes the exact request/result pair at the transparent
// forwarding boundary. Opening and syncing for each line is deliberate for
// this debug log: a proxy crash may lose at most an incomplete final write.
func (p *MCPProxyServer) writeLocalCallLog(
	_ context.Context,
	started time.Time,
	sessionID, publicName, serverName, toolName string,
	input, output interface{},
	callErr error,
) {
	p.writeScopedCallLog(nil, started, sessionID, publicName, serverName, serverName, toolName, "raw", "", "agent", input, output, callErr)
}

func (p *MCPProxyServer) writeScopedCallLog(
	_ context.Context,
	started time.Time,
	sessionID, publicName, serverID, serverName, toolName, toolKind, publicationID, caller string,
	input, output interface{},
	callErr error,
) {
	cfg := p.config
	if p.mainServer != nil {
		if current, err := p.mainServer.GetConfig(); err == nil && current != nil {
			cfg = current
		}
	}
	if cfg == nil || cfg.Logging == nil || !cfg.Logging.Enabled {
		return
	}

	filename := cfg.Logging.CallsFilename
	if filename == "" {
		filename = "calls.jsonl"
	}
	if !filepath.IsAbs(filename) {
		filename = filepath.Join(cfg.DataDir, "logs", filename)
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		p.logger.Error("failed to create call log directory")
		return
	}

	clientName, clientVersion := "", ""
	if p.sessionStore != nil && sessionID != "" {
		if info := p.sessionStore.GetSession(sessionID); info != nil {
			clientName, clientVersion = info.ClientName, info.ClientVersion
		}
	}
	record := localCallLogRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		StartedAt:      started.UTC().Format(time.RFC3339Nano),
		DurationMS:     time.Since(started).Milliseconds(),
		Client:         clientName,
		ClientVersion:  clientVersion,
		SessionID:      sessionID,
		PublicToolName: publicName,
		ServerID:       serverID,
		SourceServer:   serverName,
		OriginalTool:   toolName,
		ToolKind:       toolKind,
		PublicationID:  publicationID,
		Caller:         caller,
		Input:          input,
		Output:         output,
	}
	if callErr != nil {
		var protocolErr *mcp.JSONRPCErrorDetailsError
		if errors.As(callErr, &protocolErr) {
			record.Error = protocolErr.Details
		} else {
			record.Error = callErr.Error()
		}
		record.Output = nil
	}

	p.callLogMu.Lock()
	defer p.callLogMu.Unlock()
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		p.logger.Error("failed to open local call log")
		return
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(record); err != nil {
		p.logger.Error("failed to encode local call log record")
		return
	}
	_ = f.Sync()
}
