// Copyright 2026 Alibaba Group
// Licensed under the Apache License, Version 2.0 (the "License");

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/event/consume"
	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/event/personal"
	"github.com/spf13/cobra"
)

func TestPersonalCardEventListSchemaDryRunAndValidation(t *testing.T) {
	list := newEventListCommand()
	list.SilenceUsage = true
	list.SilenceErrors = true
	var listOut bytes.Buffer
	list.SetOut(&listOut)
	list.SetArgs([]string{"--category", "card"})
	if err := list.Execute(); err != nil {
		t.Fatalf("event list --category card error = %v", err)
	}
	if !strings.Contains(listOut.String(), personal.EventCardAction) {
		t.Fatalf("card event list missing %s:\n%s", personal.EventCardAction, listOut.String())
	}
	if strings.Contains(listOut.String(), personal.EventMention) || strings.Contains(listOut.String(), personal.EventOAApprovalTaskCreated) {
		t.Fatalf("card category list leaked another category:\n%s", listOut.String())
	}

	schema := newEventSchemaCommand()
	schema.SilenceUsage = true
	schema.SilenceErrors = true
	var schemaOut bytes.Buffer
	schema.SetOut(&schemaOut)
	schema.SetArgs([]string{personal.EventCardAction, "--flatten"})
	if err := schema.Execute(); err != nil {
		t.Fatalf("event schema %s --flatten error = %v", personal.EventCardAction, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(schemaOut.Bytes(), &doc); err != nil {
		t.Fatalf("decode card schema: %v\n%s", err, schemaOut.String())
	}
	if doc["event_key"] != personal.EventCardAction || doc["category"] != "card" || doc["rule_type"] != "all" || doc["jq_root_path"] != "." {
		t.Fatalf("card schema document = %#v", doc)
	}
	properties := doc["schema"].(map[string]any)["properties"].(map[string]any)
	if len(properties) != 5 || properties["payload"].(map[string]any)["additionalProperties"] != true {
		t.Fatalf("card schema properties = %#v", properties)
	}

	if err := validatePersonalBusinessEventOptions(personal.EventCardAction, personalConsumeOptions{}); err != nil {
		t.Fatalf("card event without target/filter options error = %v", err)
	}
	for name, opts := range map[string]personalConsumeOptions{
		"--user":             {UserID: "user-1"},
		"--open-dingtalk-id": {OpenDingTalkID: "open-user-1"},
		"--group":            {GroupID: "cid-1"},
		"--query":            {QueryCSV: "urgent"},
		"--filter-json":      {FilterJSON: `{"field":"content","op":"eq","value":"urgent"}`},
	} {
		err := validatePersonalBusinessEventOptions(personal.EventCardAction, opts)
		if err == nil || !strings.Contains(err.Error(), name+" not supported for card event "+personal.EventCardAction) {
			t.Fatalf("card %s validation error = %v", name, err)
		}
	}

	oldIdentity := personalResolveEventIdentity
	t.Cleanup(func() { personalResolveEventIdentity = oldIdentity })
	personalResolveEventIdentity = func(context.Context, string, string) (personal.Identity, error) {
		return personal.Identity{AccessToken: "token", LocalSubject: "subject", ClientID: "client", SourceID: "open"}, nil
	}
	dryRun := newEventConsumeCommand()
	dryRun.SilenceUsage = true
	dryRun.SilenceErrors = true
	var stderr bytes.Buffer
	dryRun.SetOut(io.Discard)
	dryRun.SetErr(&stderr)
	dryRun.SetArgs([]string{personal.EventCardAction, personal.EventOAApprovalTaskCreated, "--flatten", "--dry-run"})
	if err := dryRun.Execute(); err != nil {
		t.Fatalf("card multi dry-run error = %v", err)
	}
	if !strings.Contains(stderr.String(), "event_key="+personal.EventCardAction+" rule_type=all rule_param={}") {
		t.Fatalf("card dry-run did not use empty-object rule_param:\n%s", stderr.String())
	}
}

func TestPersonalCardMultiConsumeCreatesAndCleansSubscriptionsOnSharedBus(t *testing.T) {
	restoreMany := installPersonalManySeams(t)
	defer restoreMany()
	oldCreate := personalCreateSubscription
	defer func() { personalCreateSubscription = oldCreate }()
	t.Setenv("DWS_CONFIG_DIR", t.TempDir())

	personalResolveEventIdentity = func(context.Context, string, string) (personal.Identity, error) {
		return personal.Identity{AccessToken: "token", LocalSubject: "subject", ClientID: "client", SourceID: "open"}, nil
	}
	var requests []personal.CreateSubscriptionRequest
	personalCreateSubscription = func(_ *personal.Client, _ context.Context, req personal.CreateSubscriptionRequest) (*personal.Subscription, error) {
		requests = append(requests, req)
		return &personal.Subscription{SubscribeID: "sub-" + req.EventKey}, nil
	}
	personalEnsureSubscription = ensurePersonalSubscription
	personalUpsertRunState = func(string, personal.RunState) error { return nil }
	var deleted []string
	personalDeleteSubscription = func(_ *personal.Client, _ context.Context, subscribeID string) error {
		deleted = append(deleted, subscribeID)
		return nil
	}
	personalRemoveRunStates = func(string, []string) error { return nil }
	personalValidateConsumeConfig = func(consume.Config) error { return nil }
	runManyCalls := 0
	var gotSpecs []consume.ConsumerSpec
	personalConsumeRunMany = func(_ context.Context, _ consume.Config, specs []consume.ConsumerSpec) error {
		runManyCalls++
		gotSpecs = append([]consume.ConsumerSpec(nil), specs...)
		return nil
	}

	eventKeys := []string{personal.EventCardAction, personal.EventOAApprovalTaskCreated}
	if err := runPersonalEventConsume(newPersonalCoverageCommand(), personalConsumeOptions{EventKeys: eventKeys, Flatten: true}); err != nil {
		t.Fatalf("card multi consume error = %v", err)
	}
	if runManyCalls != 1 || len(requests) != 2 || len(gotSpecs) != 2 {
		t.Fatalf("shared bus calls/requests/specs = %d/%d/%d", runManyCalls, len(requests), len(gotSpecs))
	}
	if requests[0].EventKey != personal.EventCardAction || requests[0].RuleType != "all" || requests[0].RuleParam == nil || len(requests[0].RuleParam) != 0 || requests[0].Filter != nil {
		t.Fatalf("card request = %#v, want all with empty-object rule and empty filter", requests[0])
	}
	if requests[1].EventKey != personal.EventOAApprovalTaskCreated || requests[1].RuleParam == nil || len(requests[1].RuleParam) != 0 {
		t.Fatalf("OA control request changed = %#v", requests[1])
	}
	wantDeleted := []string{"sub-" + personal.EventOAApprovalTaskCreated, "sub-" + personal.EventCardAction}
	if !reflect.DeepEqual(deleted, wantDeleted) {
		t.Fatalf("cleanup subscriptions = %#v, want %#v", deleted, wantDeleted)
	}
}

func TestPersonalCardSubscriptionReuseStatusAndStopWiring(t *testing.T) {
	oldGet := personalGetSubscription
	oldStatus := eventRunPersonalStatus
	oldStop := eventRunPersonalStop
	t.Cleanup(func() {
		personalGetSubscription = oldGet
		eventRunPersonalStatus = oldStatus
		eventRunPersonalStop = oldStop
	})
	personalGetSubscription = func(*personal.Client, context.Context, string) (*personal.Subscription, error) {
		return &personal.Subscription{SubscribeID: "card-sub", EventKey: personal.EventCardAction, RuleType: "all"}, nil
	}
	sub, eventKey, ruleType, err := ensurePersonalSubscription(context.Background(), nil, personal.Identity{}, personalConsumeOptions{SubscribeID: "card-sub"})
	if err != nil {
		t.Fatalf("reuse card subscription: %v", err)
	}
	if sub.SubscribeID != "card-sub" || eventKey != personal.EventCardAction || ruleType != "all" {
		t.Fatalf("reused card subscription = %#v, event=%q rule=%q", sub, eventKey, ruleType)
	}

	var statusOpts personalStatusOptions
	eventRunPersonalStatus = func(_ *cobra.Command, opts personalStatusOptions) error {
		statusOpts = opts
		return nil
	}
	status := newEventStatusCommand()
	status.SetOut(io.Discard)
	status.SetErr(io.Discard)
	status.SetArgs([]string{"--event", personal.EventCardAction, "--subscribe-id", "card-sub"})
	if err := status.Execute(); err != nil {
		t.Fatalf("card event status error = %v", err)
	}
	if statusOpts.EventKey != personal.EventCardAction || statusOpts.SubscribeID != "card-sub" {
		t.Fatalf("card status options = %#v", statusOpts)
	}

	var stopOpts personalStopOptions
	eventRunPersonalStop = func(_ *cobra.Command, opts personalStopOptions) error {
		stopOpts = opts
		return nil
	}
	root := &cobra.Command{Use: "dws", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.PersistentFlags().Bool("yes", false, "")
	event := &cobra.Command{Use: "event"}
	event.AddCommand(newEventStopCommand())
	root.AddCommand(event)
	root.SetArgs([]string{"event", "stop", "card-sub", "--yes"})
	if err := root.Execute(); err != nil {
		t.Fatalf("card event stop error = %v", err)
	}
	if stopOpts.SubscribeID != "card-sub" || stopOpts.All {
		t.Fatalf("card stop options = %#v", stopOpts)
	}
}
