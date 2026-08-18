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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/apiclient"
	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/output"
)

func TestParseQueryStringToJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, raw, want string
	}{
		{
			name: "simple key-value",
			raw:  "timeMin=2026-04-01&maxResults=10",
			want: `{"maxResults":"10","timeMin":"2026-04-01"}`,
		},
		{
			name: "with special chars",
			raw:  "timeMin=2026-04-01T14:00:00+08:00&showDeleted=false",
			want: `{"showDeleted":"false","timeMin":"2026-04-01T14:00:00+08:00"}`,
		},
		{
			name: "empty value skipped",
			raw:  "nextToken=&syncToken=abc",
			want: `{"syncToken":"abc"}`,
		},
		{
			name: "all empty",
			raw:  "nextToken=&syncToken=",
			want: "{}",
		},
		{
			name: "empty string",
			raw:  "",
			want: "{}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseQueryStringToJSON(tt.raw)
			if got != tt.want {
				t.Errorf("parseQueryStringToJSON(%q) = %s, want %s", tt.raw, got, tt.want)
			}
		})
	}
}

func TestRunAPI_QueryStringBlocked(t *testing.T) {
	gf := &GlobalFlags{}
	cmd := newAPICommand(gf)

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	cmd.SetArgs([]string{"GET", "/v1.0/calendar/users/me/events?timeMin=2026-04-01&maxResults=10"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error when path contains query string, got nil")
	}
	errMsg := stderr.String()
	if !strings.Contains(errMsg, "--params") {
		t.Errorf("expected --params hint in error, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "maxResults") {
		t.Errorf("expected parsed query params in error, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "/v1.0/calendar/users/me/events") {
		t.Errorf("expected clean path in suggestion, got: %s", errMsg)
	}
}

func TestRunAPI_NoErrorWithoutQueryString(t *testing.T) {
	gf := &GlobalFlags{}
	cmd := newAPICommand(gf)

	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(&bytes.Buffer{})

	cmd.SetArgs([]string{"GET", "/v1.0/contact/users/me"})
	err := cmd.Execute()

	errMsg := stderr.String()
	if strings.Contains(errMsg, "查询参数") {
		t.Errorf("should not reject path without query string, got: %s", errMsg)
	}
	_ = err
}

type failingAppTokenGetter struct {
	called *bool
}

func (g failingAppTokenGetter) GetToken(context.Context) (string, error) {
	*g.called = true
	return "", errors.New("token provider must not run")
}

func TestRunAPIDryRunHasZeroCredentialFileAndNetworkSideEffects(t *testing.T) {
	oldProvider := newAppTokenProvider
	t.Cleanup(func() { newAppTokenProvider = oldProvider })
	called := false
	newAppTokenProvider = func(_, _, _ string) appTokenGetter {
		return failingAppTokenGetter{called: &called}
	}

	gf := &GlobalFlags{DryRun: true, Token: "must-not-be-shown"}
	cmd := newAPICommand(gf)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"POST", "/v1.0/example/upload",
		"--params", "@/definitely/missing/params.json",
		"--data", "@/definitely/missing/body.json",
		"--file", "media=/definitely/missing/upload.bin",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run should not open deferred inputs: %v\nstderr=%s", err, stderr.String())
	}
	if called {
		t.Fatal("dry-run called AppTokenProvider")
	}
	got := stdout.String()
	if strings.Contains(got, "must-not-be-shown") || !strings.Contains(got, "not opened") || !strings.Contains(got, "Auth:") {
		t.Fatalf("dry-run preview = %q", got)
	}
}

func TestAPIFileFlagCompatibilityAndValidation(t *testing.T) {
	gf := &GlobalFlags{DryRun: true}
	cmd := newAPICommand(gf)
	flag := cmd.Flags().Lookup("file")
	if flag == nil || flag.DefValue != "" || flag.Hidden {
		t.Fatalf("--file flag = %#v", flag)
	}

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"GET", "/v1.0/test", "--file", "demo.bin"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "GET") {
		t.Fatalf("GET --file error = %v", err)
	}

	cmd = newAPICommand(gf)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"POST", "/v1.0/test", "--params", "-", "--file", "-"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "stdin") {
		t.Fatalf("stdin conflict error = %v", err)
	}

	for _, tc := range []struct {
		name string
		gf   *GlobalFlags
		args []string
		want string
	}{
		{"output", &GlobalFlags{DryRun: true, Output: "out.bin"}, []string{"POST", "/v1.0/test", "--file", "demo.bin"}, "--output"},
		{"pagination", &GlobalFlags{DryRun: true}, []string{"POST", "/v1.0/test", "--file", "demo.bin", "--page-all"}, "--page-all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := newAPICommand(tc.gf)
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(tc.args)
			if err := command.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("file exclusion error = %v", err)
			}
		})
	}
}

func TestResolveRawAPIExplicitTokenIsTemporaryAppToken(t *testing.T) {
	got, err := resolveRawAPIToken(context.Background(), " temporary-app-token ")
	if err != nil || got != "temporary-app-token" {
		t.Fatalf("explicit App Token = %q, %v", got, err)
	}
}

type apiRoundTripper func(*http.Request) (*http.Response, error)

func (f apiRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRunPaginatedPreservesPagePayloadArray(t *testing.T) {
	client := apiclient.NewClient("app-token", "")
	page := 0
	client.HTTPClient.Transport = apiRoundTripper(func(*http.Request) (*http.Response, error) {
		page++
		body := `{"items":[{"id":"2"}],"has_more":false}`
		if page == 1 {
			body = `{"items":[{"id":"1"}],"has_more":true,"next_token":"page-2"}`
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
	var out bytes.Buffer
	err := runPaginated(context.Background(), client, apiclient.RawAPIRequest{
		Method: http.MethodGet,
		Path:   "/v1.0/example/resources",
	}, &apiFlags{pageLimit: 10, pageDelay: 1}, apiclient.ResponseOptions{
		Format: output.FormatJSON,
		Out:    &out,
		ErrOut: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	var pages []map[string]any
	if err := json.Unmarshal(out.Bytes(), &pages); err != nil {
		t.Fatalf("page array output = %s: %v", out.String(), err)
	}
	if len(pages) != 2 || pages[0]["next_token"] != "page-2" {
		t.Fatalf("page payload shape changed: %#v", pages)
	}
}
