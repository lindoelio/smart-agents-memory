// Package mcp implements the dependency-free JSON-RPC stdio server exposed
// by `smart-agents-memory mcp`. The protocol is JSON-RPC 2.0; every memory
// operation is a tool that calls the same use cases as the CLI.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/lindoelio/smart-agents-memory/internal/app"
	"github.com/lindoelio/smart-agents-memory/internal/version"
)

// Server serves the MCP protocol over a pair of streams.
type Server struct {
	App *app.App
}

// NewServer builds the server for an application.
func NewServer(a *app.App) *Server { return &Server{App: a} }

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve reads one JSON-RPC message per line and writes one response line.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			if err := encoder.Encode(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}}); err != nil {
				return err
			}
			continue
		}
		resp, send := s.handle(ctx, req)
		if !send {
			continue
		}
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *Server) handle(ctx context.Context, req request) (response, bool) {
	resp := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]string{
				"name":    version.Name,
				"version": version.Version,
			},
		}
		return resp, true
	case "notifications/initialized":
		return resp, false
	case "tools/list":
		resp.Result = map[string]any{"tools": toolDefinitions()}
		return resp, true
	case "tools/call":
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &call); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid params"}
			return resp, true
		}
		var text string
		var structured any
		var err error
		if call.Name == "memory_tree" {
			var args struct {
				CWD string `json:"cwd"`
			}
			_ = json.Unmarshal(call.Arguments, &args)
			result, recallErr := s.App.Recall(ctx, app.RecallOptions{Tree: true, CWD: args.CWD})
			err = recallErr
			if err == nil {
				text = fmt.Sprintf("revision: %s\n\n%s", result.Revision, result.Text)
				structured = map[string]any{
					"revision":      result.Revision,
					"router_prompt": result.Text,
				}
			}
		} else {
			text, err = s.call(ctx, call.Name, call.Arguments)
		}
		if err != nil {
			resp.Result = map[string]any{
				"content": []map[string]string{{"type": "text", "text": err.Error()}},
				"isError": true,
			}
			return resp, true
		}
		result := map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
		}
		if structured != nil {
			result["structuredContent"] = structured
		}
		resp.Result = result
		return resp, true
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found"}
		return resp, true
	}
}

func (s *Server) call(ctx context.Context, name string, raw json.RawMessage) (string, error) {
	switch name {
	case "memory_tree":
		var args struct {
			CWD string `json:"cwd"`
		}
		_ = json.Unmarshal(raw, &args)
		result, err := s.App.Recall(ctx, app.RecallOptions{Tree: true, CWD: args.CWD})
		return result.Text, err
	case "memory_recall":
		var args struct {
			Query      string   `json:"query"`
			Categories []string `json:"categories"`
			Refs       []string `json:"refs"`
			Limit      int      `json:"limit"`
			MaxChars   int      `json:"max_chars"`
			CWD        string   `json:"cwd"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("invalid memory_recall arguments")
		}
		result, err := s.App.Recall(ctx, app.RecallOptions{
			Categories: args.Categories, Refs: args.Refs, Query: args.Query,
			Limit: args.Limit, MaxChars: args.MaxChars, CWD: args.CWD,
		})
		return result.Text, err
	case "memory_search":
		var args struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("invalid memory_search arguments")
		}
		hits, err := s.App.Search(ctx, args.Query, args.Limit)
		if err != nil {
			return "", err
		}
		var out string
		for _, hit := range hits {
			if hit.Line > 0 {
				out += fmt.Sprintf("%s:%d: %s\n", hit.Path, hit.Line, hit.Text)
			} else {
				out += fmt.Sprintf("%s: %s\n", hit.Path, hit.Text)
			}
		}
		return out, nil
	case "memory_brief":
		var args struct {
			Project  string `json:"project"`
			Worktree string `json:"worktree"`
			Global   bool   `json:"global"`
			CWD      string `json:"cwd"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("invalid memory_brief arguments")
		}
		return s.App.Brief(ctx, app.BriefOptions{
			Project: args.Project, Worktree: args.Worktree, Global: args.Global, CWD: args.CWD,
		})
	case "memory_add":
		var args struct {
			Scope              string   `json:"scope"`
			Title              string   `json:"title"`
			Body               string   `json:"body"`
			Source             string   `json:"source"`
			Native             string   `json:"native"`
			Tier               string   `json:"tier"`
			Pinned             bool     `json:"pinned"`
			Project            string   `json:"project"`
			Worktree           string   `json:"worktree"`
			WorktreeAuto       bool     `json:"worktree_auto"`
			Team               bool     `json:"team"`
			Category           string   `json:"category"`
			Scenarios          []string `json:"scenarios"`
			Description        string   `json:"description"`
			Reason             string   `json:"reason"`
			Tags               []string `json:"tags"`
			Force              bool     `json:"force"`
			AllowOtherLanguage bool     `json:"allow_other_language"`
			CWD                string   `json:"cwd"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("invalid memory_add arguments")
		}
		return s.App.Add(ctx, app.AddOptions{
			Scope: args.Scope, Title: args.Title, Body: args.Body, Source: args.Source,
			Native: args.Native, Tier: args.Tier, Pinned: args.Pinned, Project: args.Project,
			Worktree: args.Worktree, WorktreeAuto: args.WorktreeAuto, Team: args.Team,
			Category: args.Category, UsageScenarios: args.Scenarios, Description: args.Description,
			Reason: args.Reason, Tags: args.Tags, Force: args.Force,
			AllowOtherLanguage: args.AllowOtherLanguage, CWD: args.CWD,
		})
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func toolDefinitions() []map[string]any {
	text := map[string]any{"type": "string"}
	return []map[string]any{
		{
			"name":        "memory_tree",
			"description": "Return the compact memory tree router.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"cwd": text}},
		},
		{
			"name":        "memory_recall",
			"description": "Return a focused subtree, exact refs or a ranked query.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"query": text, "categories": map[string]any{"type": "array", "items": text},
				"refs": map[string]any{"type": "array", "items": text}, "limit": map[string]any{"type": "integer"},
				"max_chars": map[string]any{"type": "integer"}, "cwd": text,
			}},
		},
		{
			"name":        "memory_search",
			"description": "Search the corpus and team memory.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"query": text, "limit": map[string]any{"type": "integer"},
			}},
		},
		{
			"name":        "memory_brief",
			"description": "Return the hot tier, project/worktree and team context.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"project": text, "worktree": text, "global": map[string]any{"type": "boolean"}, "cwd": text,
			}},
		},
		{
			"name":        "memory_add",
			"description": "Record one memory, protected by SecretGuard and pinned rules.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"scope": text, "title": text, "body": text, "team": map[string]any{"type": "boolean"},
				"tier": text, "pinned": map[string]any{"type": "boolean"}, "category": text,
				"scenarios": map[string]any{"type": "array", "items": text}, "tags": map[string]any{"type": "array", "items": text},
				"description": text, "reason": text, "project": text, "worktree": text,
				"worktree_auto": map[string]any{"type": "boolean"}, "force": map[string]any{"type": "boolean"}, "cwd": text,
			}},
		},
	}
}
