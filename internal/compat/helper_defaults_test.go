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
	"testing"

	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/market"
	"github.com/spf13/cobra"
)

// TestAddHelperDefault_UnknownCmdReturnsFalse covers the "cmd is nil or
// never went through NewDirectCommand" path — the compat package cannot
// resolve bindings without prior registration.
func TestAddHelperDefault_UnknownCmdReturnsFalse(t *testing.T) {
	t.Parallel()

	stray := &cobra.Command{Use: "stray"}
	if AddHelperDefault(stray, "whatever", "42") {
		t.Fatal("expected false for unregistered cmd")
	}
	if AddHelperDefault(nil, "whatever", "42") {
		t.Fatal("expected false for nil cmd")
	}
}

// TestAddHelperDefault_UnknownFlagReturnsFalse verifies that a registered
// route still rejects flag names that don't correspond to any binding.
// This is the "envelope tool doesn't declare this flag" backstop — the
// MCP backend would reject an unknown key.
func TestAddHelperDefault_UnknownFlagReturnsFalse(t *testing.T) {
	t.Parallel()

	bindings := []FlagBinding{
		{FlagName: "page", Property: "pageNum", Kind: ValueInt},
	}
	cmd := &cobra.Command{Use: "list"}
	registerRouteForHelperDefaults(cmd, bindings)
	t.Cleanup(func() { routeRegistry.Delete(cmd) })

	if AddHelperDefault(cmd, "bogus", "1") {
		t.Fatal("expected false for unknown flag name")
	}
}

// TestAddHelperDefault_EnvelopeDefaultTakesPrecedence locks in the
// backwards-compat contract: if the envelope already declared a Default
// for the matched Property, AddHelperDefault no-ops.
func TestAddHelperDefault_EnvelopeDefaultTakesPrecedence(t *testing.T) {
	t.Parallel()

	bindings := []FlagBinding{
		{FlagName: "page", Property: "pageNum", Kind: ValueInt, Default: "5"},
	}
	cmd := &cobra.Command{Use: "list"}
	registerRouteForHelperDefaults(cmd, bindings)
	t.Cleanup(func() { routeRegistry.Delete(cmd) })

	if AddHelperDefault(cmd, "page", "1") {
		t.Fatal("expected AddHelperDefault to refuse envelope-claimed default")
	}
	if got := helperDefaultsFor(cmd); len(got) != 0 {
		t.Fatalf("expected no helper defaults registered, got %+v", got)
	}
}

// TestAddHelperDefault_MatchesPrimaryAndAlias exercises the three lookup
// paths that mirror firstChangedFlag: FlagName, Alias, Aliases.
func TestAddHelperDefault_MatchesPrimaryAndAlias(t *testing.T) {
	t.Parallel()

	bindings := []FlagBinding{
		{FlagName: "pageNum", Alias: "page", Aliases: []string{"p"}, Property: "pageNum", Kind: ValueInt},
	}
	cases := []struct {
		name     string
		flagName string
	}{
		{"by primary", "pageNum"},
		{"by alias", "page"},
		{"by extra alias", "p"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "list"}
			registerRouteForHelperDefaults(cmd, bindings)
			t.Cleanup(func() { routeRegistry.Delete(cmd) })

			if !AddHelperDefault(cmd, tc.flagName, "7") {
				t.Fatalf("expected AddHelperDefault to match via %s", tc.flagName)
			}
			entries := helperDefaultsFor(cmd)
			if len(entries) != 1 || entries[0].paramName != "pageNum" ||
				entries[0].defaultValue != "7" || entries[0].kind != ValueInt {
				t.Fatalf("unexpected helper defaults: %+v", entries)
			}
		})
	}
}

// TestAddHelperDefault_FirstRegistrationWins verifies the idempotency
// guarantee: repeated AddHelperDefault calls for the same property leave
// the first value untouched.
func TestAddHelperDefault_FirstRegistrationWins(t *testing.T) {
	t.Parallel()

	bindings := []FlagBinding{
		{FlagName: "page", Property: "pageNum", Kind: ValueInt},
	}
	cmd := &cobra.Command{Use: "list"}
	registerRouteForHelperDefaults(cmd, bindings)
	t.Cleanup(func() { routeRegistry.Delete(cmd) })

	if !AddHelperDefault(cmd, "page", "1") {
		t.Fatal("first registration should succeed")
	}
	if AddHelperDefault(cmd, "page", "9") {
		t.Fatal("second registration should no-op")
	}
	entries := helperDefaultsFor(cmd)
	if len(entries) != 1 || entries[0].defaultValue != "1" {
		t.Fatalf("expected first value to stick, got %+v", entries)
	}
}

// TestNormalizer_HelperDefaultAppliedWhenEnvelopeMissing exercises the
// full compat chain: no envelope Default declared, AddHelperDefault
// registers a value, and the normalizer writes it into params under the
// correct ValueKind.
func TestNormalizer_HelperDefaultAppliedWhenEnvelopeMissing(t *testing.T) {
	t.Parallel()

	override := market.CLIToolOverride{
		CLIName: "list",
		Flags: map[string]market.CLIFlagOverride{
			"pageNum": {Alias: "page", Type: "int"},
			"tags":    {Alias: "tags", Type: "string_slice"},
		},
	}
	bindings, normalizer := buildOverrideBindings(override)
	if normalizer == nil {
		t.Fatal("expected non-nil normalizer")
	}

	cmd := &cobra.Command{Use: "list"}
	ApplyBindings(cmd, bindings)
	registerRouteForHelperDefaults(cmd, bindings)
	t.Cleanup(func() { routeRegistry.Delete(cmd) })

	if !AddHelperDefault(cmd, "page", "1") {
		t.Fatal("AddHelperDefault(page) returned false")
	}
	if !AddHelperDefault(cmd, "tags", "a,b") {
		t.Fatal("AddHelperDefault(tags) returned false")
	}

	params, err := CollectBindings(cmd, bindings, nil)
	if err != nil {
		t.Fatalf("CollectBindings: %v", err)
	}
	if err := normalizer(cmd, params); err != nil {
		t.Fatalf("normalizer: %v", err)
	}

	if got, ok := params["pageNum"].(int); !ok || got != 1 {
		t.Fatalf("pageNum: expected int(1), got %T(%v)", params["pageNum"], params["pageNum"])
	}
	tags, ok := params["tags"].([]string)
	if !ok || len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Fatalf("tags: expected [a b], got %T(%v)", params["tags"], params["tags"])
	}
}

// TestNormalizer_EnvelopeDefaultStillWins asserts that when the envelope
// declares its own Default, the envelope value wins even if the app layer
// also tried to register a helper default (AddHelperDefault refuses).
func TestNormalizer_EnvelopeDefaultStillWins(t *testing.T) {
	t.Parallel()

	override := market.CLIToolOverride{
		CLIName: "list",
		Flags: map[string]market.CLIFlagOverride{
			"pageNum": {Alias: "page", Type: "int", Default: "42"},
		},
	}
	bindings, normalizer := buildOverrideBindings(override)
	cmd := &cobra.Command{Use: "list"}
	ApplyBindings(cmd, bindings)
	registerRouteForHelperDefaults(cmd, bindings)
	t.Cleanup(func() { routeRegistry.Delete(cmd) })

	if AddHelperDefault(cmd, "page", "1") {
		t.Fatal("envelope-claimed default must refuse helper registration")
	}

	params, err := CollectBindings(cmd, bindings, nil)
	if err != nil {
		t.Fatalf("CollectBindings: %v", err)
	}
	if err := normalizer(cmd, params); err != nil {
		t.Fatalf("normalizer: %v", err)
	}
	if got, ok := params["pageNum"].(int); !ok || got != 42 {
		t.Fatalf("expected envelope default 42, got %T(%v)", params["pageNum"], params["pageNum"])
	}
}

// TestNormalizer_UserChangedFlagStillWins verifies the full precedence:
// user input > envelope default > helper default. With no envelope
// default declared, we register a helper fallback AND set the flag from
// argv — the argv value wins.
func TestNormalizer_UserChangedFlagStillWins(t *testing.T) {
	t.Parallel()

	override := market.CLIToolOverride{
		CLIName: "list",
		Flags: map[string]market.CLIFlagOverride{
			"pageNum": {Alias: "page", Type: "int"},
		},
	}
	bindings, normalizer := buildOverrideBindings(override)
	cmd := &cobra.Command{Use: "list"}
	ApplyBindings(cmd, bindings)
	registerRouteForHelperDefaults(cmd, bindings)
	t.Cleanup(func() { routeRegistry.Delete(cmd) })

	if !AddHelperDefault(cmd, "page", "1") {
		t.Fatal("AddHelperDefault returned false")
	}
	if err := cmd.ParseFlags([]string{"--page", "9"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	params, err := CollectBindings(cmd, bindings, nil)
	if err != nil {
		t.Fatalf("CollectBindings: %v", err)
	}
	if err := normalizer(cmd, params); err != nil {
		t.Fatalf("normalizer: %v", err)
	}
	if got, ok := params["pageNum"].(int); !ok || got != 9 {
		t.Fatalf("expected user value 9, got %T(%v)", params["pageNum"], params["pageNum"])
	}
}
