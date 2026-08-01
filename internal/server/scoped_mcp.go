package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
	"go.uber.org/zap"
)

type scopedMCPServers struct {
	ID         string
	Name       string
	RawCount   int
	Agent      *mcpserver.MCPServer
	Controller *mcpserver.MCPServer
}

func stableServerID(server *config.ServerConfig) string {
	if server == nil {
		return ""
	}
	if strings.TrimSpace(server.ID) != "" {
		return server.ID
	}
	return server.Name
}

// RefreshScopedServers atomically rebuilds the per-upstream tool catalogs.
// MCP server instances are retained so existing Streamable HTTP sessions keep
// working while SetTools swaps their catalogs.
func (p *MCPProxyServer) RefreshScopedServers() {
	if p.mainServer == nil {
		return
	}
	cfg, err := p.mainServer.GetConfig()
	if err != nil || cfg == nil {
		p.logger.Error("failed to read config for scoped MCP endpoints", zap.Error(err))
		return
	}
	discovered, err := p.upstreamManager.DiscoverTools(context.Background())
	if err != nil {
		p.logger.Warn("failed to discover scoped MCP tools", zap.Error(err))
		discovered = nil
	}
	byServer := map[string][]*config.ToolMetadata{}
	for _, tool := range discovered {
		if tool != nil {
			copy := *tool
			copy.Name = strings.TrimPrefix(copy.Name, copy.ServerName+":")
			byServer[copy.ServerName] = append(byServer[copy.ServerName], &copy)
		}
	}

	p.scopedServersMu.Lock()
	defer p.scopedServersMu.Unlock()
	if p.scopedServers == nil {
		p.scopedServers = map[string]*scopedMCPServers{}
	}
	active := map[string]bool{}
	for _, serverCfg := range cfg.Servers {
		if serverCfg == nil {
			continue
		}
		id := stableServerID(serverCfg)
		active[id] = true
		scoped := p.scopedServers[id]
		if scoped == nil {
			opts := []mcpserver.ServerOption{mcpserver.WithToolCapabilities(true), mcpserver.WithRecovery()}
			if p.hooks != nil {
				opts = append(opts, mcpserver.WithHooks(p.hooks))
			}
			scoped = &scopedMCPServers{
				ID: id, Name: serverCfg.Name,
				Agent:      mcpserver.NewMCPServer(serverCfg.Name, mcpServerVersion(), opts...),
				Controller: mcpserver.NewMCPServer(serverCfg.Name+" controller", mcpServerVersion(), opts...),
			}
			p.scopedServers[id] = scoped
		}
		scoped.Name = serverCfg.Name
		agentTools, controllerTools := p.buildScopedTools(serverCfg, byServer[serverCfg.Name])
		scoped.RawCount = len(byServer[serverCfg.Name])
		scoped.Agent.SetTools(agentTools...)
		scoped.Controller.SetTools(controllerTools...)
	}
	for id := range p.scopedServers {
		if !active[id] {
			delete(p.scopedServers, id)
		}
	}
}

func (p *MCPProxyServer) buildScopedTools(serverCfg *config.ServerConfig, tools []*config.ToolMetadata) ([]mcpserver.ServerTool, []mcpserver.ServerTool) {
	hidden := map[string]bool{}
	for _, name := range serverCfg.AgentHiddenTools {
		hidden[name] = true
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	agent := make([]mcpserver.ServerTool, 0, len(tools)+len(serverCfg.Publications))
	controller := make([]mcpserver.ServerTool, 0, len(tools))
	names := map[string]bool{}
	serverID := stableServerID(serverCfg)
	for _, tool := range tools {
		definition := buildTransparentDirectTool(tool, tool.Name)
		handler := p.makeScopedRawHandler(serverID, serverCfg.Name, tool.Name, "controller")
		controller = append(controller, mcpserver.ServerTool{Tool: definition, Handler: handler})
		names[tool.Name] = true
		if !hidden[tool.Name] {
			agent = append(agent, mcpserver.ServerTool{
				Tool:    definition,
				Handler: p.makeScopedRawHandler(serverID, serverCfg.Name, tool.Name, "agent"),
			})
		}
	}
	for _, publication := range serverCfg.Publications {
		if names[publication.Name] {
			p.logger.Error("published tool collides with raw tool", zap.String("server_id", serverID), zap.String("tool", publication.Name))
			continue
		}
		var definition mcp.Tool
		if err := json.Unmarshal(publication.Tool, &definition); err != nil {
			p.logger.Error("invalid published tool descriptor", zap.String("publication_id", publication.PublicationID), zap.Error(err))
			continue
		}
		definition.Name = publication.Name
		agent = append(agent, mcpserver.ServerTool{
			Tool:    definition,
			Handler: p.makePublicationHandler(serverCfg, publication),
		})
		names[publication.Name] = true
	}
	return agent, controller
}

func (p *MCPProxyServer) makeScopedRawHandler(serverID, serverName, toolName, caller string) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		started := time.Now()
		sessionID := sessionIDFromContext(ctx)
		arguments := request.GetArguments()
		result, err := p.upstreamManager.CallTool(ctx, serverName+":"+toolName, arguments)
		if err != nil {
			p.writeScopedCallLog(ctx, started, sessionID, request.Params.Name, serverID, serverName, toolName, "raw", "", caller, arguments, nil, err)
			return nil, err
		}
		forwarded, ok := result.(*mcp.CallToolResult)
		if !ok || forwarded == nil {
			err = fmt.Errorf("upstream returned unexpected result type %T", result)
			p.writeScopedCallLog(ctx, started, sessionID, request.Params.Name, serverID, serverName, toolName, "raw", "", caller, arguments, nil, err)
			return nil, err
		}
		p.writeScopedCallLog(ctx, started, sessionID, request.Params.Name, serverID, serverName, toolName, "raw", "", caller, arguments, forwarded, nil)
		return forwarded, nil
	}
}

type publicationCallRequest struct {
	ServerID      string                 `json:"server_id"`
	PublicationID string                 `json:"publication_id"`
	Arguments     map[string]interface{} `json:"arguments"`
	SessionID     string                 `json:"session_id,omitempty"`
	ClientName    string                 `json:"client_name,omitempty"`
	ClientVersion string                 `json:"client_version,omitempty"`
}

type publicationCallResponse struct {
	Result *mcp.CallToolResult      `json:"result,omitempty"`
	Error  *mcp.JSONRPCErrorDetails `json:"error,omitempty"`
}

func (p *MCPProxyServer) makePublicationHandler(serverCfg *config.ServerConfig, publication config.PublishedToolConfig) mcpserver.ToolHandlerFunc {
	serverID, serverName := stableServerID(serverCfg), serverCfg.Name
	callback := serverCfg.PublicationCallback
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		started := time.Now()
		sessionID := sessionIDFromContext(ctx)
		arguments := request.GetArguments()
		if callback == nil || callback.URL == "" || callback.Token == "" {
			err := fmt.Errorf("controller publication callback is unavailable")
			p.writeScopedCallLog(ctx, started, sessionID, request.Params.Name, serverID, serverName, publication.Name, publication.Kind, publication.PublicationID, "agent", arguments, nil, err)
			return nil, err
		}
		clientName, clientVersion := p.sessionClientInfo(sessionID)
		payload, _ := json.Marshal(publicationCallRequest{
			ServerID: serverID, PublicationID: publication.PublicationID,
			Arguments: arguments, SessionID: sessionID,
			ClientName: clientName, ClientVersion: clientVersion,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, callback.URL, bytes.NewReader(payload))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+callback.Token)
			var response *http.Response
			response, err = (&http.Client{Timeout: 30 * time.Minute}).Do(req)
			if err == nil {
				defer response.Body.Close()
				body, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<20))
				if readErr != nil {
					err = readErr
				} else {
					var decoded publicationCallResponse
					if decodeErr := json.Unmarshal(body, &decoded); decodeErr != nil {
						err = fmt.Errorf("controller returned invalid publication response: %w", decodeErr)
					} else if decoded.Error != nil {
						err = &mcp.JSONRPCErrorDetailsError{Details: *decoded.Error}
					} else if response.StatusCode >= 400 || decoded.Result == nil {
						err = fmt.Errorf("controller publication call failed with HTTP %d", response.StatusCode)
					} else {
						p.writeScopedCallLog(ctx, started, sessionID, request.Params.Name, serverID, serverName, publication.Name, publication.Kind, publication.PublicationID, "agent", arguments, decoded.Result, nil)
						return decoded.Result, nil
					}
				}
			}
		}
		p.writeScopedCallLog(ctx, started, sessionID, request.Params.Name, serverID, serverName, publication.Name, publication.Kind, publication.PublicationID, "agent", arguments, nil, err)
		return nil, err
	}
}

func (p *MCPProxyServer) sessionClientInfo(sessionID string) (string, string) {
	if p.sessionStore != nil && sessionID != "" {
		if info := p.sessionStore.GetSession(sessionID); info != nil {
			return info.ClientName, info.ClientVersion
		}
	}
	return "", ""
}

func (p *MCPProxyServer) GetScopedServer(serverID string, controller bool) *mcpserver.MCPServer {
	p.scopedServersMu.RLock()
	value := p.scopedServers[serverID]
	needsRefresh := value == nil || value.RawCount == 0
	p.scopedServersMu.RUnlock()
	// Upstream initialization finishes asynchronously after the HTTP listener
	// is ready. The first client retries discovery when startup built an empty
	// catalog; established non-empty catalogs do not pay this cost per call.
	if needsRefresh {
		p.RefreshScopedServers()
	}
	p.scopedServersMu.RLock()
	defer p.scopedServersMu.RUnlock()
	value = p.scopedServers[serverID]
	if value == nil {
		return nil
	}
	if controller {
		return value.Controller
	}
	return value.Agent
}
