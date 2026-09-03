// Copyright 2026 Alibaba Group
// Licensed under the Apache License, Version 2.0 (the "License");

package personal

import (
	"reflect"
	"testing"

	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/event/transport"
)

func TestCardActionEventOutputPreservesUnknownBusinessPayload(t *testing.T) {
	ev := transport.Event{
		EventID:       "outer-event",
		EventBornTime: 1788200000000,
		EventType:     EventCardAction,
		SubscribeID:   "outer-sub",
		Data: `{
			"eventId":"card-event",
			"eventKey":"user_card_action_triggered",
			"occurredAtMs":1788200000123,
			"subId":"inner-sub",
			"payload":{
				"callbackType":"future_callback_type",
				"futureField":{"nested":true},
				"uid":"transport-user",
				"clientId":"transport-client",
				"body":{"uid":"business-user","unknown":42}
			}
		}`,
	}

	projected, err := ProjectOutput(ev)
	if err != nil {
		t.Fatalf("ProjectOutput() error = %v", err)
	}
	got, ok := projected.(CardActionEventOutput)
	if !ok {
		t.Fatalf("ProjectOutput() type = %T, want CardActionEventOutput", projected)
	}
	if got.Type != EventCardAction || got.EventID != "card-event" || got.Timestamp != 1788200000123 || got.SubscribeID != "outer-sub" {
		t.Fatalf("common fields = %#v", got)
	}
	if got.Payload["callbackType"] != "future_callback_type" || !reflect.DeepEqual(got.Payload["futureField"], map[string]any{"nested": true}) {
		t.Fatalf("unknown business fields were not preserved: %#v", got.Payload)
	}
	if _, ok := got.Payload["uid"]; ok {
		t.Fatalf("payload retained top-level transport uid: %#v", got.Payload)
	}
	if _, ok := got.Payload["clientId"]; ok {
		t.Fatalf("payload retained top-level transport clientId: %#v", got.Payload)
	}
	body, ok := got.Payload["body"].(map[string]any)
	if !ok || body["uid"] != "business-user" || body["unknown"] != float64(42) {
		t.Fatalf("nested business payload changed: %#v", got.Payload["body"])
	}

	transportProjected, err := ProjectTransportOutput(ev)
	if err != nil {
		t.Fatalf("ProjectTransportOutput() error = %v", err)
	}
	if !reflect.DeepEqual(transportProjected, ev) {
		t.Fatalf("default transport envelope changed: %#v", transportProjected)
	}
}
