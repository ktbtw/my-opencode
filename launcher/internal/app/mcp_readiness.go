package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"launcher/internal/model"
)

const (
	semanticMCPReadinessTimeout      = 90 * time.Second
	semanticMCPReadinessPollInterval = 750 * time.Millisecond
	semanticMCPReadinessRequestTime  = 8 * time.Second
)

type semanticMCPReadinessTarget struct {
	ItemID string
	Name   string
}

type semanticMCPReadinessFailure struct {
	ItemID string
	Name   string
	Reason string
}

type semanticMCPReadinessError struct {
	Failures []semanticMCPReadinessFailure
	Cause    error
}

func (e *semanticMCPReadinessError) Error() string {
	parts := make([]string, 0, len(e.Failures))
	for _, failure := range e.Failures {
		parts = append(parts, failure.Name+"："+failure.Reason)
	}
	if len(parts) == 0 && e.Cause != nil {
		parts = append(parts, e.Cause.Error())
	}
	if len(parts) == 0 {
		parts = append(parts, "unknown")
	}
	return "MCP 工具就绪校验失败：" + strings.Join(parts, "；")
}

func semanticMCPReadinessTargets(profile *model.SemanticAgentProfile) []semanticMCPReadinessTarget {
	items := semanticPreflightMCPItems(profile)
	out := make([]semanticMCPReadinessTarget, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		name := strings.TrimSpace(item.name)
		if name == "" {
			name = strings.TrimSpace(item.id)
		}
		key := strings.ToLower(name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, semanticMCPReadinessTarget{ItemID: item.id, Name: name})
	}
	return out
}

func waitForSemanticMCPReadiness(ctx context.Context, port int, targets []semanticMCPReadinessTarget) error {
	if len(targets) == 0 {
		return nil
	}
	var lastServers []model.MCPServerStatusInfo
	var lastTools map[string][]model.MCPToolInfo
	var lastRequestErr error
	for {
		servers, tools, err := fetchMCPReadinessSnapshot(ctx, port)
		if err == nil {
			lastServers = servers
			lastTools = tools
			lastRequestErr = nil
			if ready, _ := assessSemanticMCPReadiness(targets, servers, tools); ready {
				return nil
			}
		} else {
			lastRequestErr = err
		}

		select {
		case <-ctx.Done():
			failures := semanticMCPReadinessFailures(targets, lastServers, lastTools)
			if len(failures) == 0 && lastRequestErr != nil {
				return &semanticMCPReadinessError{Cause: lastRequestErr}
			}
			if len(failures) == 0 {
				return &semanticMCPReadinessError{Cause: ctx.Err()}
			}
			return &semanticMCPReadinessError{Failures: failures}
		case <-time.After(semanticMCPReadinessPollInterval):
		}
	}
}

func fetchMCPReadinessSnapshot(ctx context.Context, port int) ([]model.MCPServerStatusInfo, map[string][]model.MCPToolInfo, error) {
	statusCtx, statusCancel := context.WithTimeout(ctx, semanticMCPReadinessRequestTime)
	servers, err := fetchMCPStatus(statusCtx, port)
	statusCancel()
	if err != nil {
		return nil, nil, fmt.Errorf("读取 MCP 状态失败：%w", err)
	}
	toolsCtx, toolsCancel := context.WithTimeout(ctx, semanticMCPReadinessRequestTime)
	tools, err := fetchMCPTools(toolsCtx, port)
	toolsCancel()
	if err != nil {
		return servers, nil, fmt.Errorf("读取 MCP 工具失败：%w", err)
	}
	return servers, tools, nil
}

func assessSemanticMCPReadiness(targets []semanticMCPReadinessTarget, servers []model.MCPServerStatusInfo, tools map[string][]model.MCPToolInfo) (bool, []string) {
	targetFailures := semanticMCPReadinessFailures(targets, servers, tools)
	failures := make([]string, 0, len(targetFailures))
	for _, failure := range targetFailures {
		failures = append(failures, failure.Name+"："+failure.Reason)
	}
	return len(failures) == 0, failures
}

func semanticMCPReadinessFailures(targets []semanticMCPReadinessTarget, servers []model.MCPServerStatusInfo, tools map[string][]model.MCPToolInfo) []semanticMCPReadinessFailure {
	serverByName := make(map[string]model.MCPServerStatusInfo, len(servers))
	for _, server := range servers {
		serverByName[strings.ToLower(strings.TrimSpace(server.Name))] = server
	}
	toolsByName := make(map[string][]model.MCPToolInfo, len(tools))
	for name, items := range tools {
		toolsByName[strings.ToLower(strings.TrimSpace(name))] = items
	}

	failures := make([]semanticMCPReadinessFailure, 0)
	for _, target := range targets {
		key := strings.ToLower(strings.TrimSpace(target.Name))
		server, ok := serverByName[key]
		if !ok {
			failures = append(failures, semanticMCPReadinessFailure{ItemID: target.ItemID, Name: target.Name, Reason: "状态缺失"})
			continue
		}
		status := strings.ToLower(strings.TrimSpace(server.Status))
		if status != "connected" {
			reason := strings.TrimSpace(server.Error)
			if reason == "" {
				reason = "状态=" + firstNonEmpty(status, "unknown")
			}
			failures = append(failures, semanticMCPReadinessFailure{ItemID: target.ItemID, Name: target.Name, Reason: reason})
			continue
		}
		if len(toolsByName[key]) == 0 {
			failures = append(failures, semanticMCPReadinessFailure{ItemID: target.ItemID, Name: target.Name, Reason: "已连接但未暴露工具"})
		}
	}
	return failures
}

func semanticMCPReadinessItemFailures(err error) (map[string]string, bool) {
	var readinessErr *semanticMCPReadinessError
	if !errors.As(err, &readinessErr) || len(readinessErr.Failures) == 0 {
		return nil, false
	}
	out := make(map[string]string, len(readinessErr.Failures))
	for _, failure := range readinessErr.Failures {
		out[failure.ItemID] = "MCP 工具就绪校验失败：" + failure.Name + "：" + failure.Reason
	}
	return out, true
}
