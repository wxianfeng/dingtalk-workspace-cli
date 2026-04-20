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

package compat

import (
	"context"
	"encoding/json"

	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/executor"
	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/pkg/edition"
	"github.com/spf13/cobra"
)

// compatToolCaller bridges executor.Runner to edition.ToolCaller for use
// inside ReplaceRunE handlers. Mirrors internal/app/tool_caller_adapter.go
// but reads global --format / --dry-run via cobra root persistent flags so
// the compat package avoids an internal/app dependency.
type compatToolCaller struct {
	cmd    *cobra.Command
	runner executor.Runner
}

func newToolCallerAdapter(cmd *cobra.Command, runner executor.Runner) edition.ToolCaller {
	return &compatToolCaller{cmd: cmd, runner: runner}
}

func (c *compatToolCaller) CallTool(ctx context.Context, productID, toolName string, args map[string]any) (*edition.ToolResult, error) {
	inv := executor.NewHelperInvocation("overlay."+productID+"."+toolName, productID, toolName, args)
	if c.DryRun() {
		inv.DryRun = true
	}
	result, err := c.runner.Run(ctx, inv)
	if err != nil {
		return nil, err
	}
	return convertCompatResult(result), nil
}

func (c *compatToolCaller) Format() string {
	if c.cmd == nil {
		return "json"
	}
	root := c.cmd.Root()
	if root == nil {
		return "json"
	}
	if f := root.PersistentFlags().Lookup("format"); f != nil && f.Value != nil {
		if v := f.Value.String(); v != "" {
			return v
		}
	}
	return "json"
}

func (c *compatToolCaller) DryRun() bool {
	if c.cmd == nil {
		return false
	}
	root := c.cmd.Root()
	if root == nil {
		return false
	}
	v, _ := root.PersistentFlags().GetBool("dry-run")
	return v
}

func convertCompatResult(r executor.Result) *edition.ToolResult {
	resp := r.Response
	if resp == nil {
		return &edition.ToolResult{}
	}
	contentRaw, ok := resp["content"]
	if !ok {
		data, _ := json.Marshal(resp)
		return &edition.ToolResult{
			Content: []edition.ContentBlock{{Type: "text", Text: string(data)}},
		}
	}
	switch v := contentRaw.(type) {
	case []any:
		blocks := make([]edition.ContentBlock, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			blocks = append(blocks, edition.ContentBlock{
				Type: strVal(m, "type"),
				Text: strVal(m, "text"),
			})
		}
		return &edition.ToolResult{Content: blocks}
	case map[string]any:
		data, _ := json.Marshal(v)
		return &edition.ToolResult{
			Content: []edition.ContentBlock{{Type: "text", Text: string(data)}},
		}
	default:
		data, _ := json.Marshal(contentRaw)
		return &edition.ToolResult{
			Content: []edition.ContentBlock{{Type: "text", Text: string(data)}},
		}
	}
}

func strVal(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
