// Copyright 2026 Alibaba Group
// Licensed under the Apache License, Version 2.0 (the "License");

package personal

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/event/transport"
)

func TestCardActionEventOutputPreservesReviewedCallbackShapeAndTypes(t *testing.T) {
	ev := transport.Event{
		EventID:       "outer-event",
		EventBornTime: 1788441872000,
		EventType:     EventCardAction,
		SubscribeID:   "sub-card-example",
		Data: `{
			"eventId":"card-event-example",
			"eventKey":"user_card_action_triggered",
			"occurredAtMs":1788441872239,
			"subId":"inner-sub-example",
			"payload":{
				"body":{
					"actionData":{"context":{
						"answers":{
							"q0":{"custom":"","selected":["o1"]},
							"q2":{"selected":[]}
						},
						"createUid":"user-create-example",
						"orgId":"org-example",
						"outcome":"answered",
						"questions":[{
							"allowCustom":true,
							"header":"会议标题",
							"id":"q0",
							"options":[{"description":"项目相关讨论会议","id":"o1","label":"项目讨论"}],
							"prompt":"请问会议的标题是什么？",
							"selection":"single"
						},{
							"allowCustom":false,
							"header":"参会人",
							"id":"q2",
							"inputKind":"person",
							"options":[],
							"prompt":"需要邀请哪些参会人？",
							"selection":"multiple"
						}],
						"sourceProjectionVersion":"dingtalk-coding-surface-v1",
						"sourceTurnId":"turn-example",
						"futureContext":{"kept":true}
					}},
					"bizInfoDTO":{"appKey":"app-example","bizId":"card-example"},
					"context":{
						"answers":"{\"q0\":{\"custom\":\"\",\"selected\":[\"o1\"]}}",
						"createUid":"user-create-example",
						"orgId":"org-example",
						"outcome":"answered",
						"questions":"[{\"id\":\"q0\"}]",
						"sourceProjectionVersion":"dingtalk-coding-surface-v1",
						"sourceTurnId":"turn-example"
					},
					"conversationContextDTO":{"cid":"conversation-example"},
					"extension":{"spaceModel":"{\"spaces\":{}}","futureExtension":"opaque"},
					"operatorDTO":{"operatorUserAgent":"TestClient/1.0","uid":10001},
					"spaceId":"space-example",
					"spaceType":"im_single",
					"triggerTimestamp":1788441872151,
					"futureBody":{"kept":true}
				},
				"event_time":1788441872152,
				"futurePayload":"kept"
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
	if got.Type != EventCardAction || got.EventID != "card-event-example" || got.Timestamp != 1788441872239 || got.SubscribeID != "sub-card-example" {
		t.Fatalf("common fields = %#v", got)
	}
	if got.Payload["event_time"] != float64(1788441872152) || got.Payload["futurePayload"] != "kept" {
		t.Fatalf("payload fields = %#v", got.Payload)
	}
	body := requireCardMap(t, got.Payload["body"], "payload.body")
	if body["spaceId"] != "space-example" || body["spaceType"] != "im_single" || body["triggerTimestamp"] != float64(1788441872151) {
		t.Fatalf("body routing/time fields = %#v", body)
	}
	if !reflect.DeepEqual(body["futureBody"], map[string]any{"kept": true}) {
		t.Fatalf("unknown body field changed: %#v", body["futureBody"])
	}

	actionData := requireCardMap(t, body["actionData"], "payload.body.actionData")
	context := requireCardMap(t, actionData["context"], "payload.body.actionData.context")
	if context["createUid"] != "user-create-example" || context["orgId"] != "org-example" || context["outcome"] != "answered" {
		t.Fatalf("structured context identifiers/outcome = %#v", context)
	}
	if context["sourceProjectionVersion"] != "dingtalk-coding-surface-v1" || context["sourceTurnId"] != "turn-example" {
		t.Fatalf("structured context source fields = %#v", context)
	}
	if !reflect.DeepEqual(context["futureContext"], map[string]any{"kept": true}) {
		t.Fatalf("unknown context field changed: %#v", context["futureContext"])
	}
	answers := requireCardMap(t, context["answers"], "payload.body.actionData.context.answers")
	q0 := requireCardMap(t, answers["q0"], "answers.q0")
	q2 := requireCardMap(t, answers["q2"], "answers.q2")
	if q0["custom"] != "" || !reflect.DeepEqual(q0["selected"], []any{"o1"}) || !reflect.DeepEqual(q2["selected"], []any{}) {
		t.Fatalf("answers = %#v", answers)
	}
	questions, ok := context["questions"].([]any)
	if !ok || len(questions) != 2 {
		t.Fatalf("questions = %#v", context["questions"])
	}
	q0Definition := requireCardMap(t, questions[0], "questions[0]")
	if q0Definition["id"] != "q0" || q0Definition["header"] != "会议标题" || q0Definition["prompt"] != "请问会议的标题是什么？" ||
		q0Definition["selection"] != "single" || q0Definition["allowCustom"] != true {
		t.Fatalf("question definition = %#v", q0Definition)
	}
	options, ok := q0Definition["options"].([]any)
	if !ok || len(options) != 1 {
		t.Fatalf("question options = %#v", q0Definition["options"])
	}
	option := requireCardMap(t, options[0], "questions[0].options[0]")
	if option["id"] != "o1" || option["label"] != "项目讨论" || option["description"] != "项目相关讨论会议" {
		t.Fatalf("question option = %#v", option)
	}
	q2Definition := requireCardMap(t, questions[1], "questions[1]")
	if q2Definition["id"] != "q2" || q2Definition["inputKind"] != "person" || q2Definition["selection"] != "multiple" || q2Definition["allowCustom"] != false {
		t.Fatalf("person question definition = %#v", q2Definition)
	}

	legacyContext := requireCardMap(t, body["context"], "payload.body.context")
	if legacyContext["answers"] != `{"q0":{"custom":"","selected":["o1"]}}` || legacyContext["questions"] != `[{"id":"q0"}]` {
		t.Fatalf("stringified context changed: %#v", legacyContext)
	}
	operator := requireCardMap(t, body["operatorDTO"], "payload.body.operatorDTO")
	if operator["uid"] != float64(10001) || operator["operatorUserAgent"] != "TestClient/1.0" {
		t.Fatalf("operator = %#v", operator)
	}
	if _, ok := context["createUid"].(string); !ok {
		t.Fatalf("createUid type = %T, want string", context["createUid"])
	}
	if _, ok := context["orgId"].(string); !ok {
		t.Fatalf("orgId type = %T, want string", context["orgId"])
	}
	if _, ok := operator["uid"].(float64); !ok {
		t.Fatalf("operatorDTO.uid decoded type = %T, want JSON number", operator["uid"])
	}
	bizInfo := requireCardMap(t, body["bizInfoDTO"], "payload.body.bizInfoDTO")
	if bizInfo["appKey"] != "app-example" || bizInfo["bizId"] != "card-example" ||
		requireCardMap(t, body["conversationContextDTO"], "payload.body.conversationContextDTO")["cid"] != "conversation-example" {
		t.Fatalf("business/conversation fields = %#v/%#v", body["bizInfoDTO"], body["conversationContextDTO"])
	}
	extension := requireCardMap(t, body["extension"], "payload.body.extension")
	if extension["futureExtension"] != "opaque" || extension["spaceModel"] != `{"spaces":{}}` {
		t.Fatalf("extension fields = %#v", body["extension"])
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var rendered map[string]any
	if err := json.Unmarshal(raw, &rendered); err != nil {
		t.Fatalf("Unmarshal(rendered) error = %v", err)
	}
	if len(rendered) != 5 {
		t.Fatalf("flattened top-level fields = %#v, want exactly five", rendered)
	}
	for _, name := range []string{"type", "event_id", "timestamp", "subscribe_id", "payload"} {
		if _, ok := rendered[name]; !ok {
			t.Fatalf("flattened output missing exact top-level field %q: %#v", name, rendered)
		}
	}
}

func requireCardMap(t *testing.T, value any, path string) map[string]any {
	t.Helper()
	got, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", path, value)
	}
	return got
}

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
