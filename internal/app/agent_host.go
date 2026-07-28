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

package app

import (
	"regexp"
	"strings"

	apperrors "github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/errors"
	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/pkg/configmeta"
)

const (
	envDWSAgentHost    = "DWS_AGENT_HOST"
	headerDWSAgentHost = "x-dws-agent-host"
)

var agentHostPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func init() {
	configmeta.Register(configmeta.ConfigItem{
		Name:        envDWSAgentHost,
		Category:    configmeta.CategoryExternal,
		Description: "调用 DWS 的 Agent 宿主标识；仅用于日志和 BI 观测",
		Example:     "qwenwork_cloud",
	})
}

// parseAgentHost normalizes and validates the caller-provided observation
// label. CR/LF is rejected before trimming so it can never be hidden at the
// edge of a value. An unset or whitespace-only value means "do not emit".
func parseAgentHost(raw string) (string, error) {
	if strings.ContainsAny(raw, "\r\n") {
		return "", invalidAgentHostError()
	}

	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if !agentHostPattern.MatchString(value) {
		return "", invalidAgentHostError()
	}
	return value, nil
}

func invalidAgentHostError() error {
	// Do not include the raw environment value in the error: it is an
	// untrusted caller-controlled string and may contain sensitive data.
	return apperrors.NewValidation(
		"DWS_AGENT_HOST must match ^[a-z0-9][a-z0-9_-]*$",
		apperrors.WithReason("invalid_agent_host"),
	)
}
