// Copyright 2026 Alibaba Group
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package edition provides an extension-point mechanism that allows private
// overlay modules (e.g. an internal distribution) to customise CLI behaviour
// without modifying the open-source core. The open-source build uses the
// zero-value defaults; an overlay calls Override before Execute.
package edition

import (
	"context"
	"sync"

	"github.com/spf13/cobra"
)

// ServerInfo describes a static MCP server endpoint.
type ServerInfo struct {
	ID       string
	Name     string
	Endpoint string
	Prefixes []string
}

// ContentBlock is a single content item in a ToolResult.
type ContentBlock struct {
	Type string
	Text string
}

// ToolResult holds the response from an MCP tool call.
type ToolResult struct {
	Content []ContentBlock
}

// ToolCaller abstracts MCP tool invocation so private overlays can call MCP
// tools without importing internal packages. The open-source core provides a
// concrete adapter wrapping executor.Runner.
type ToolCaller interface {
	// CallTool invokes an MCP tool by product ID and tool name.
	CallTool(ctx context.Context, productID, toolName string, args map[string]any) (*ToolResult, error)
	// Format returns the current output format ("json", "table", "raw").
	Format() string
	// DryRun returns true when --dry-run is active.
	DryRun() bool
}

// ReplaceRunECtx carries the collected flag set, pre-normalized params, and
// MCP caller into a Replace-RunE handler. Handlers only implement non-
// declarable business logic; all envelope-declared flag parsing / transform /
// omitWhen / runtimeDefault has already run by the time the handler executes.
//
// See discovery-schema-v3 §2.4.
type ReplaceRunECtx struct {
	// ServerID is the canonical MCP product ID this leaf targets. Reflects
	// CLIToolOverride.ServerOverride when set, otherwise the enclosing
	// CLIOverlay.ID.
	ServerID string
	// ToolName is the MCP tool name declared in the envelope's toolOverrides.
	ToolName string
	// Params is the normalized body ready for MCP invocation. Handlers may
	// mutate it before calling Caller.CallTool.
	Params map[string]any
	// Caller is the MCP invocation facade.
	Caller ToolCaller
}

// ReplaceRunEFn is the handler signature resolved from CLIToolOverride.ReplaceRunE.
// It is invoked in place of the default envelope → MCP invocation on the leaf.
type ReplaceRunEFn func(cmd *cobra.Command, rctx ReplaceRunECtx) error

// RuntimeDefaultFn resolves a single runtimeDefault placeholder (e.g.
// "$currentUserId") to a concrete string value. Called lazily at RunE time.
// Returning (_, false) is equivalent to "not registered" and falls through
// to the next-lower default source.
type RuntimeDefaultFn func(ctx context.Context) (string, bool)

// Hooks groups all edition-specific behavioural overrides. Zero values
// fall back to open-source defaults so the struct is safe to use as-is.
type Hooks struct {
	// --- identity ---
	Name         string // "open" (default) / overlay identifier
	ScenarioCode string // injected into x-dingtalk-scenario-code header

	// --- runtime mode ---
	IsEmbedded     bool // true when running inside a host application
	HideAuthLogin  bool // true suppresses the "dws auth login" command
	AutoPurgeToken bool // true deletes local token data on expiry

	// --- paths ---
	ConfigDir func() string // custom config directory; nil → ~/.dws

	// --- HTTP headers ---
	MergeHeaders func(base map[string]string) map[string]string

	// --- auth hooks ---
	OnAuthError   func(configDir string, err error) error
	TokenProvider func(ctx context.Context, fallback func() (string, error)) (string, error)

	// --- token persistence (overlay-only) ---
	// When non-nil, these override the default keychain-based token storage.
	// The data parameter is JSON-serialized TokenData.
	SaveToken   func(configDir string, data []byte) error
	LoadToken   func(configDir string) ([]byte, error)
	DeleteToken func(configDir string) error

	// --- auth credentials (overlay-only) ---
	AuthClientID      string // non-empty overrides DefaultClientID
	AuthClientFromMCP bool   // true routes OAuth through MCP endpoints

	// --- product & endpoint ---
	StaticServers         func() []ServerInfo                          // non-nil → skip Market discovery
	VisibleProducts       func() []string                              // non-nil → override help visibility
	RegisterExtraCommands func(root *cobra.Command, caller ToolCaller) // register overlay-only commands

	// --- discovery ---

	// DiscoveryURL overrides the Market API endpoint for server list.
	// Non-empty → loadDynamicCommands uses FetchServersFromURL(DiscoveryURL)
	// instead of the default Market base URL. Provides edition-level isolation.
	DiscoveryURL string

	// DiscoveryHeaders returns HTTP headers injected into discovery requests.
	// Used to authenticate edition-specific endpoints.
	DiscoveryHeaders func() map[string]string

	// SupplementServers returns edition-specific MCP servers NOT registered
	// in any Market registry. Always merged into the endpoint map alongside
	// Market/cache results, regardless of discovery success or failure.
	SupplementServers func() []ServerInfo

	// FallbackServers returns the full server list as a safety net when
	// Market discovery + cache both fail. Results are NOT cached so the
	// next startup still attempts live discovery.
	FallbackServers func() []ServerInfo

	// AfterPersistentPreRun runs at the end of the root PersistentPreRunE after
	// global setup (OAuth flag overrides, log level, output sink). Overlays use
	// this for clients that bypass the MCP runner (e.g. A2A gateway).
	AfterPersistentPreRun func(cmd *cobra.Command, args []string) error

	// ClassifyToolResult is called before the framework's default business-error
	// detection on MCP tool results. If it returns a non-nil error, that error
	// is used instead of the generic CategoryAPI business error. Editions use
	// this to return custom error types with specific exit codes (e.g. PAT
	// authorization errors with exit code 4).
	ClassifyToolResult func(content map[string]any) error

	// --- schema v3: Replace-RunE + runtime defaults ---

	// ReplaceRunEHandler resolves a named RunE handler declared by
	// CLIToolOverride.ReplaceRunE. Returning nil means "no handler" and the
	// leaf keeps its default envelope-based MCP invocation. Open-source core
	// always returns nil; overlays register handlers via this hook.
	ReplaceRunEHandler func(id string) ReplaceRunEFn

	// RuntimeDefaults returns resolvers for runtimeDefault placeholders (e.g.
	// "$currentUserId" → fn). Placeholders not in the map fall through to a
	// "not registered" warning. Open-source core returns an empty map;
	// overlays populate the whitelist ($currentUserId / $unionId / $corpId /
	// $now / $today). See schema v3 §2.3.
	RuntimeDefaults func() map[string]RuntimeDefaultFn
}

var (
	mu      sync.RWMutex
	current = defaultHooks()
)

// Get returns the active edition hooks (never nil).
func Get() *Hooks {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Override replaces the active edition hooks. Must be called before Execute.
func Override(h *Hooks) {
	if h == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	current = h
}
