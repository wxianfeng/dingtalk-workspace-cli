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
	"context"
	"testing"

	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/compat"
	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/executor"
	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/market"
	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/pkg/cmdutil"
	"github.com/spf13/cobra"
)

// captureInvocationRunner records the params of the last invocation so
// end-to-end merge tests can assert the helper default fallback reaches
// the MCP payload. Lives here rather than in compat tests because it's
// only used by the cross-package pagination regression.
type captureInvocationRunner struct {
	lastTool   string
	lastParams map[string]any
}

func (c *captureInvocationRunner) Run(_ context.Context, inv executor.Invocation) (executor.Result, error) {
	c.lastTool = inv.Tool
	c.lastParams = inv.Params
	return executor.Result{Invocation: inv}, nil
}

// stringSink is an io.Writer that discards everything; used to swallow
// cobra output during test Execute calls without polluting test logs.
type stringSink struct{}

func (stringSink) Write(p []byte) (int, error) { return len(p), nil }

// envelopeCmd builds a dynamic root stamped with the envelope provenance
// annotation that BuildDynamicCommands applies in production via
// cmdutil.MarkEnvelopeSource. Tests that want to exercise the
// same-name/envelope-authoritative branch must use this constructor;
// otherwise mergeDynamicWithHelpers takes the defensive shadow path.
func envelopeCmd(use string) *cobra.Command {
	c := &cobra.Command{Use: use}
	cmdutil.MarkEnvelopeSource(c)
	return c
}

func findChild(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// TestMergeDynamicWithHelpers_DynamicLeafWinsOverHelperLeaf preserves the
// invariant introduced in PR #156: when the envelope declares a leaf, a
// same-named hardcoded helper leaf must not displace it. Under
// cmdutil.MergeHardcodedLeaves this is encoded as "leaf/leaf → dynamic wins".
func TestMergeDynamicWithHelpers_DynamicLeafWinsOverHelperLeaf(t *testing.T) {
	dynRoot := envelopeCmd("todo")
	dynTask := &cobra.Command{Use: "task"}
	dynList := &cobra.Command{Use: "list", Short: "dynamic list"}
	dynTask.AddCommand(dynList)
	dynRoot.AddCommand(dynTask)

	helperRoot := &cobra.Command{Use: "todo"}
	helperTask := &cobra.Command{Use: "task"}
	helperList := &cobra.Command{Use: "list", Short: "helper list"}
	helperList.Flags().String("page", "1", "page")
	helperList.Flags().String("size", "20", "size")
	helperTask.AddCommand(helperList)
	helperRoot.AddCommand(helperTask)

	out := mergeDynamicWithHelpers([]*cobra.Command{dynRoot}, []*cobra.Command{helperRoot})

	if len(out) != 1 || out[0] != dynRoot {
		t.Fatalf("out = %v, want single dynRoot", out)
	}
	task := findChild(dynRoot, "task")
	if task != dynTask {
		t.Fatalf("todo.task = %v, want dynamic instance", task)
	}
	list := findChild(task, "list")
	if list != dynList {
		t.Fatalf("todo.task.list = %v, want dynamic instance (helper leaf must not displace)", list)
	}
	if list.Short != "dynamic list" {
		t.Fatalf("todo.task.list.Short = %q, want dynamic short", list.Short)
	}
}

// TestMergeDynamicWithHelpers_HelperOnlySubtreeGraftedOntoDynamic covers the
// regression this plan targets: when a helper declares a sibling command the
// envelope omitted, it must be grafted onto the dynamic subtree instead of
// being discarded along with the helper root.
func TestMergeDynamicWithHelpers_HelperOnlySubtreeGraftedOntoDynamic(t *testing.T) {
	dynRoot := envelopeCmd("todo")
	dynTask := &cobra.Command{Use: "task"}
	dynTask.AddCommand(&cobra.Command{Use: "list"})
	dynRoot.AddCommand(dynTask)

	helperRoot := &cobra.Command{Use: "todo"}
	helperTask := &cobra.Command{Use: "task"}
	helperTask.AddCommand(&cobra.Command{Use: "list"})
	helperArchive := &cobra.Command{Use: "archive"}
	helperTask.AddCommand(helperArchive)
	helperRoot.AddCommand(helperTask)

	out := mergeDynamicWithHelpers([]*cobra.Command{dynRoot}, []*cobra.Command{helperRoot})

	if len(out) != 1 || out[0] != dynRoot {
		t.Fatalf("out = %v, want single dynRoot", out)
	}
	grafted := findChild(dynTask, "archive")
	if grafted == nil {
		t.Fatalf("todo.task.archive not grafted onto dynamic subtree; children=%v", dynTask.Commands())
	}
	if grafted != helperArchive {
		t.Fatalf("todo.task.archive identity mismatch: got %p, want helper %p", grafted, helperArchive)
	}
	if grafted.Parent() != dynTask {
		t.Fatalf("todo.task.archive.Parent() = %v, want dynamic task group", grafted.Parent())
	}
}

// TestMergeDynamicWithHelpers_HelpersFillUncoveredProducts keeps the pre-PR
// #156 fallback behaviour: helpers whose top-level name has no dynamic
// counterpart are retained as whole trees.
func TestMergeDynamicWithHelpers_HelpersFillUncoveredProducts(t *testing.T) {
	dyn := envelopeCmd("todo")

	todoHelper := &cobra.Command{Use: "todo"}
	attendanceHelper := &cobra.Command{Use: "attendance"}
	chatHelper := &cobra.Command{Use: "chat"}

	out := mergeDynamicWithHelpers(
		[]*cobra.Command{dyn},
		[]*cobra.Command{todoHelper, attendanceHelper, chatHelper},
	)

	names := make(map[string]*cobra.Command, len(out))
	for _, c := range out {
		names[c.Name()] = c
	}
	if names["todo"] != dyn {
		t.Fatalf("todo = %v, want dynamic", names["todo"])
	}
	if names["attendance"] != attendanceHelper {
		t.Fatalf("attendance not preserved from helpers")
	}
	if names["chat"] != chatHelper {
		t.Fatalf("chat not preserved from helpers")
	}
	if len(out) != 3 {
		t.Fatalf("got %d commands, want 3 (todo+attendance+chat)", len(out))
	}
}

// TestMergeDynamicWithHelpers_EmptyDynamicPreservesHelpers covers the
// degenerate case where discovery is empty: helpers must pass through
// unchanged with identity and order preserved.
func TestMergeDynamicWithHelpers_EmptyDynamicPreservesHelpers(t *testing.T) {
	todoHelper := &cobra.Command{Use: "todo"}
	chatHelper := &cobra.Command{Use: "chat"}

	out := mergeDynamicWithHelpers(nil, []*cobra.Command{todoHelper, chatHelper})

	if len(out) != 2 {
		t.Fatalf("got %d commands, want 2", len(out))
	}
	if out[0] != todoHelper || out[1] != chatHelper {
		t.Fatalf("helpers order or identity changed")
	}
}

// TestMergeDynamicWithHelpers_NilEntriesSkipped guards against nil commands
// leaking in from a misbehaving factory.
func TestMergeDynamicWithHelpers_NilEntriesSkipped(t *testing.T) {
	dyn := envelopeCmd("todo")
	hlp := &cobra.Command{Use: "chat"}

	out := mergeDynamicWithHelpers(
		[]*cobra.Command{nil, dyn},
		[]*cobra.Command{nil, hlp},
	)

	if len(out) != 2 {
		t.Fatalf("got %d commands, want 2 (nils filtered)", len(out))
	}
	if out[0] != dyn || out[1] != hlp {
		t.Fatalf("unexpected ordering or identity after nil filter")
	}
}

// TestMergeDynamicWithHelpers_NonEnvelopeDynamicSkipsHelperMerge covers the
// defensive branch: if a same-named dynamic root ever lands without the
// envelope provenance annotation, the helper is dropped rather than grafted,
// so helper leaves cannot silently outrank a non-authoritative dynamic root.
func TestMergeDynamicWithHelpers_NonEnvelopeDynamicSkipsHelperMerge(t *testing.T) {
	dyn := &cobra.Command{Use: "todo"}
	dyn.AddCommand(&cobra.Command{Use: "task"})

	helperRoot := &cobra.Command{Use: "todo"}
	helperTask := &cobra.Command{Use: "task"}
	helperArchive := &cobra.Command{Use: "archive"}
	helperTask.AddCommand(helperArchive)
	helperRoot.AddCommand(helperTask)

	out := mergeDynamicWithHelpers([]*cobra.Command{dyn}, []*cobra.Command{helperRoot})

	if len(out) != 1 || out[0] != dyn {
		t.Fatalf("out = %v, want single non-envelope dyn", out)
	}
	task := findChild(dyn, "task")
	if task == nil {
		t.Fatalf("dynamic task group disappeared")
	}
	if findChild(task, "archive") != nil {
		t.Fatalf("helper archive must not be grafted onto non-envelope dynamic root")
	}
	if helperArchive.Parent() != helperTask {
		t.Fatalf("helper archive parent changed: got %v, want helperTask", helperArchive.Parent())
	}
}

// envelopeLeaf constructs a dyn leaf whose routeRegistry entry matches what
// compat.NewDirectCommand would create, so walkLeafPairs can drive
// AddHelperDefault end-to-end without wiring a real executor.Runner.
// Mirrors compat.ApplyBindings + registerRouteForHelperDefaults so the
// leaf behaves like an envelope-sourced command for the merge walker.
func envelopeLeaf(use string, bindings []compat.FlagBinding) *cobra.Command {
	c := &cobra.Command{Use: use}
	cmdutil.MarkEnvelopeSource(c)
	compat.ApplyBindings(c, bindings)
	compat.RegisterRouteForHelperDefaultsForTesting(c, bindings)
	return c
}

// TestMergeDynamicWithHelpers_RegistersHelperDefaultsForLeafPair is the
// §v3.3 regression test: when a hardcoded helper leaf declares
// Flag.DefValue for a parameter the envelope did not declare a Default
// for, mergeDynamicWithHelpers must register it as a fallback via
// compat.AddHelperDefault so the normalizer injects it into the MCP
// payload at RunE time.
func TestMergeDynamicWithHelpers_RegistersHelperDefaultsForLeafPair(t *testing.T) {
	bindings := []compat.FlagBinding{
		{FlagName: "pageNum", Alias: "page", Property: "pageNum", Kind: compat.ValueInt},
		{FlagName: "pageSize", Alias: "size", Property: "pageSize", Kind: compat.ValueInt},
	}
	dynRoot := envelopeCmd("todo")
	dynTask := &cobra.Command{Use: "task"}
	dynList := envelopeLeaf("list", bindings)
	dynTask.AddCommand(dynList)
	dynRoot.AddCommand(dynTask)

	helperRoot := &cobra.Command{Use: "todo"}
	helperTask := &cobra.Command{Use: "task"}
	helperList := &cobra.Command{Use: "list"}
	helperList.Flags().String("page", "1", "页码")
	helperList.Flags().String("size", "20", "数量")
	helperTask.AddCommand(helperList)
	helperRoot.AddCommand(helperTask)

	mergeDynamicWithHelpers([]*cobra.Command{dynRoot}, []*cobra.Command{helperRoot})

	got := compat.HelperDefaultsSnapshotForTesting(dynList)
	if got["pageNum"] != "1" {
		t.Fatalf("pageNum fallback = %q, want \"1\"; snapshot=%v", got["pageNum"], got)
	}
	if got["pageSize"] != "20" {
		t.Fatalf("pageSize fallback = %q, want \"20\"; snapshot=%v", got["pageSize"], got)
	}
}

// TestMergeDynamicWithHelpers_SkipsBoolFalseDefaults locks in the
// "bool+false is indistinguishable from zero value" guard. A helper flag
// `--dry-run` defaulting to false must not trigger registration, because
// pflag cannot tell "explicit default false" from "no opinion".
func TestMergeDynamicWithHelpers_SkipsBoolFalseDefaults(t *testing.T) {
	bindings := []compat.FlagBinding{
		{FlagName: "dryRun", Alias: "dry-run", Property: "dryRun", Kind: compat.ValueBool},
	}
	dynRoot := envelopeCmd("todo")
	dynTask := &cobra.Command{Use: "task"}
	dynList := envelopeLeaf("list", bindings)
	dynTask.AddCommand(dynList)
	dynRoot.AddCommand(dynTask)

	helperRoot := &cobra.Command{Use: "todo"}
	helperTask := &cobra.Command{Use: "task"}
	helperList := &cobra.Command{Use: "list"}
	helperList.Flags().Bool("dry-run", false, "dry run")
	helperTask.AddCommand(helperList)
	helperRoot.AddCommand(helperTask)

	mergeDynamicWithHelpers([]*cobra.Command{dynRoot}, []*cobra.Command{helperRoot})

	if got := compat.HelperDefaultsSnapshotForTesting(dynList); len(got) != 0 {
		t.Fatalf("bool=false helper default must not register; got %v", got)
	}
}

// TestMergeDynamicWithHelpers_SkipsWhenEnvelopeHasDefault covers the
// envelope-authority contract: if the envelope leaf already carries a
// cobra Flag.DefValue (mirroring flagOverride.Default), the helper
// fallback must not overwrite it. Two guards enforce this — walkLeafPairs
// filters on envFlag.DefValue != "", and AddHelperDefault rechecks the
// routeDefaults.envelopeClaimed set.
func TestMergeDynamicWithHelpers_SkipsWhenEnvelopeHasDefault(t *testing.T) {
	bindings := []compat.FlagBinding{
		{FlagName: "pageNum", Alias: "page", Property: "pageNum", Kind: compat.ValueInt, Default: "5"},
	}
	dynRoot := envelopeCmd("todo")
	dynTask := &cobra.Command{Use: "task"}
	dynList := envelopeLeaf("list", bindings)
	dynTask.AddCommand(dynList)
	dynRoot.AddCommand(dynTask)

	helperRoot := &cobra.Command{Use: "todo"}
	helperTask := &cobra.Command{Use: "task"}
	helperList := &cobra.Command{Use: "list"}
	helperList.Flags().String("page", "1", "页码")
	helperTask.AddCommand(helperList)
	helperRoot.AddCommand(helperTask)

	mergeDynamicWithHelpers([]*cobra.Command{dynRoot}, []*cobra.Command{helperRoot})

	if got := compat.HelperDefaultsSnapshotForTesting(dynList); len(got) != 0 {
		t.Fatalf("envelope Default must refuse helper fallback; got %v", got)
	}
}

// TestMergeDynamicWithHelpers_SkipsWhenEnvelopeLacksFlag covers the
// envelope-schema guard: if the helper side declares a flag the envelope
// side does not, we must not register it — the MCP backend would reject
// an unknown key. This specifically targets the case where a helper
// evolved independently of the envelope contract.
func TestMergeDynamicWithHelpers_SkipsWhenEnvelopeLacksFlag(t *testing.T) {
	bindings := []compat.FlagBinding{
		{FlagName: "pageNum", Alias: "page", Property: "pageNum", Kind: compat.ValueInt},
	}
	dynRoot := envelopeCmd("todo")
	dynTask := &cobra.Command{Use: "task"}
	dynList := envelopeLeaf("list", bindings)
	dynTask.AddCommand(dynList)
	dynRoot.AddCommand(dynTask)

	helperRoot := &cobra.Command{Use: "todo"}
	helperTask := &cobra.Command{Use: "task"}
	helperList := &cobra.Command{Use: "list"}
	helperList.Flags().String("page", "1", "页码")
	helperList.Flags().String("extra", "hello", "helper-only flag")
	helperTask.AddCommand(helperList)
	helperRoot.AddCommand(helperTask)

	mergeDynamicWithHelpers([]*cobra.Command{dynRoot}, []*cobra.Command{helperRoot})

	got := compat.HelperDefaultsSnapshotForTesting(dynList)
	if _, found := got["extra"]; found {
		t.Fatalf("helper-only extra flag must not register: %v", got)
	}
	if got["pageNum"] != "1" {
		t.Fatalf("pageNum fallback should still register: %v", got)
	}
}

// TestMergeDynamicWithHelpers_NormalizerInjectsHelperPagination is the
// open-source `todo task list --debug` end-to-end regression: with an
// envelope that declares pageNum / pageSize but leaves their Default
// empty, the helper leaf's `--page 1 --size 20` must propagate into the
// MCP invocation params when the user omits both flags.
//
// Builds a real dyn tree via compat.BuildDynamicCommands so the full
// RunE path (CollectBindings + normalizer with envelope/env/runtime/
// helper default chain + transforms) executes as in production, then
// captures the invocation via a stubbed runner.
func TestMergeDynamicWithHelpers_NormalizerInjectsHelperPagination(t *testing.T) {
	runner := &captureInvocationRunner{}
	servers := []market.ServerDescriptor{
		{
			Endpoint: "https://endpoint-todo",
			CLI: market.CLIOverlay{
				ID:      "todo",
				Command: "todo",
				Groups:  map[string]market.CLIGroupDef{"task": {Description: "任务"}},
				ToolOverrides: map[string]market.CLIToolOverride{
					"get_user_todos_in_current_org": {
						CLIName: "list",
						Group:   "task",
						Flags: map[string]market.CLIFlagOverride{
							"pageNum":  {Alias: "page", Type: "int"},
							"pageSize": {Alias: "size", Type: "int"},
						},
					},
				},
			},
		},
	}
	dynamicCmds := compat.BuildDynamicCommands(servers, runner, nil)
	if len(dynamicCmds) != 1 {
		t.Fatalf("expected 1 top-level dynamic cmd, got %d", len(dynamicCmds))
	}
	dynRoot := dynamicCmds[0]

	helperRoot := &cobra.Command{Use: "todo"}
	helperTask := &cobra.Command{Use: "task"}
	helperList := &cobra.Command{Use: "list"}
	helperList.Flags().String("page", "1", "页码")
	helperList.Flags().String("size", "20", "数量")
	helperTask.AddCommand(helperList)
	helperRoot.AddCommand(helperTask)

	mergeDynamicWithHelpers([]*cobra.Command{dynRoot}, []*cobra.Command{helperRoot})

	dynRoot.SetArgs([]string{"task", "list"})
	dynRoot.SetOut(new(stringSink))
	dynRoot.SetErr(new(stringSink))
	if err := dynRoot.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if runner.lastTool != "get_user_todos_in_current_org" {
		t.Fatalf("runner.lastTool = %q, want get_user_todos_in_current_org", runner.lastTool)
	}
	got, ok := runner.lastParams["pageNum"].(int)
	if !ok || got != 1 {
		t.Fatalf("params[pageNum] = %T(%v), want int(1)", runner.lastParams["pageNum"], runner.lastParams["pageNum"])
	}
	got2, ok := runner.lastParams["pageSize"].(int)
	if !ok || got2 != 20 {
		t.Fatalf("params[pageSize] = %T(%v), want int(20)", runner.lastParams["pageSize"], runner.lastParams["pageSize"])
	}
}

