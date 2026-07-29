// Copyright 2026 Alibaba Group
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	apperrors "github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/errors"
	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/shortcut"
)

// MessagesSend is the identity-aware common text/Markdown entry point. Media
// upload remains on the native message send command until DWS has one shared
// upload contract; this Shortcut does not pretend webhook/bot/current-user
// transports support identical capabilities.
var MessagesSend = shortcut.Shortcut{
	Service:     "chat",
	Command:     "+messages-send",
	Product:     "chat",
	Description: "统一发送文本/Markdown（current user、bot、webhook）",
	Intent:      "当你希望用同一个入口选择 current-user、bot 或 webhook 身份发送文本/Markdown 时使用；命令会按身份校验目标和凭据并路由到真实下层。current-user 支持幂等键，bot 支持群聊或批量单聊，webhook 的目标由 token 所在群决定。媒体上传仍使用原生命令。",
	Risk:        shortcut.RiskWrite,
	Flags: []shortcut.Flag{
		{Name: "identity", Type: shortcut.FlagString, Default: "user", Enum: []string{"user", "bot", "webhook"}, Desc: "发送身份"},
		{Name: "as", Type: shortcut.FlagString, Enum: []string{"user", "bot", "webhook"}, Desc: "--identity 的 lark-cli 对齐别名"},
		{Name: "group", Type: shortcut.FlagString, Desc: "群 openConversationId（user/bot 群聊）"},
		{Name: "chat-id", Type: shortcut.FlagString, Desc: "--group 的 lark-cli 对齐别名"},
		{Name: "open-dingtalk-id", Type: shortcut.FlagString, Desc: "单聊接收者 openDingTalkId（user）"},
		{Name: "users", Type: shortcut.FlagStringSlice, Desc: "批量单聊接收者 userId（bot）"},
		{Name: "open-dingtalk-ids", Type: shortcut.FlagStringSlice, Desc: "批量单聊接收者 openDingTalkId（bot）"},
		{Name: "robot-code", Type: shortcut.FlagString, Desc: "机器人 Code（bot 必填）"},
		{Name: "webhook-token", Type: shortcut.FlagString, Desc: "自定义机器人 Webhook token（webhook 必填）"},
		{Name: "title", Type: shortcut.FlagString, Desc: "消息标题（不传则从正文生成）"},
		{Name: "text", Type: shortcut.FlagString, Desc: "纯文本正文"},
		{Name: "markdown", Type: shortcut.FlagString, Desc: "Markdown 正文"},
		{Name: "uuid", Type: shortcut.FlagString, Desc: "幂等键（仅 user）"},
		{Name: "idempotency-key", Type: shortcut.FlagString, Desc: "--uuid 的 lark-cli 对齐别名（仅 user）"},
		{Name: "at-open-dingtalk-ids", Type: shortcut.FlagStringSlice, Desc: "@ 的 openDingTalkId（user/bot 群聊）"},
		{Name: "at-user-ids", Type: shortcut.FlagStringSlice, Desc: "@ 的 userId（bot/webhook）"},
		{Name: "at-mobiles", Type: shortcut.FlagStringSlice, Desc: "@ 的手机号（webhook）"},
		{Name: "at-all", Type: shortcut.FlagBool, Desc: "@所有人"},
		shortcut.AIMessageTagFlag(),
	},
	Constraints: []shortcut.Constraint{
		{Kind: shortcut.ConstraintAtLeastOne, Flags: []string{"text", "markdown"}},
		{Kind: shortcut.ConstraintMutuallyExclusive, Flags: []string{"text", "markdown"}},
		{Kind: shortcut.ConstraintMutuallyExclusive, Flags: []string{"identity", "as"}},
		{Kind: shortcut.ConstraintMutuallyExclusive, Flags: []string{"group", "chat-id"}},
		{Kind: shortcut.ConstraintMutuallyExclusive, Flags: []string{"uuid", "idempotency-key"}},
		{
			Kind:        shortcut.ConstraintCustom,
			Flags:       []string{"identity", "as", "group", "chat-id", "open-dingtalk-id", "users", "open-dingtalk-ids", "robot-code", "webhook-token", "uuid", "idempotency-key"},
			Description: "user 必须指定 group/open-dingtalk-id 之一；bot 必须指定 robot-code 和群聊/批量单聊目标之一；webhook 必须指定 webhook-token；幂等键仅 user 支持",
		},
	},
	Tips: []string{
		`dws chat +messages-send --as user --chat-id <openConversationId> --markdown "## 周报" --idempotency-key <key>`,
		`dws chat +messages-send --as bot --robot-code <robotCode> --users userId1,userId2 --text "请提交周报"`,
	},
	Validate: validateMessagesSend,
	Execute:  executeMessagesSend,
}

func validateMessagesSend(rt *shortcut.RuntimeContext) error {
	identity := messagesSendIdentity(rt)
	group := rt.StrFirst("chat-id", "group")
	openID := rt.Str("open-dingtalk-id")
	users := uniqueShortcutStrings(rt.StrSlice("users"))
	openIDs := uniqueShortcutStrings(rt.StrSlice("open-dingtalk-ids"))
	atOpenIDs := uniqueShortcutStrings(rt.StrSlice("at-open-dingtalk-ids"))
	atUserIDs := uniqueShortcutStrings(rt.StrSlice("at-user-ids"))
	atMobiles := uniqueShortcutStrings(rt.StrSlice("at-mobiles"))
	switch identity {
	case "user":
		if (group == "") == (openID == "") {
			return apperrors.NewValidation("--identity user 时 --group 与 --open-dingtalk-id 必须且只能指定一个")
		}
		if len(users) > 0 || len(openIDs) > 0 || rt.Str("robot-code") != "" || rt.Str("webhook-token") != "" {
			return apperrors.NewValidation("--identity user 不接受 bot/webhook 凭据或批量目标")
		}
		if len(atUserIDs) > 0 || len(atMobiles) > 0 {
			return apperrors.NewValidation("--identity user 只接受 --at-open-dingtalk-ids")
		}
		if openID != "" && (len(atOpenIDs) > 0 || rt.Bool("at-all")) {
			return apperrors.NewValidation("user 单聊不接受 @ 参数；@ 只适用于群聊")
		}
	case "bot":
		if rt.Str("robot-code") == "" {
			return apperrors.NewValidation("--identity bot 必须指定 --robot-code")
		}
		hasDirect := len(users)+len(openIDs) > 0
		if (group != "") == hasDirect {
			return apperrors.NewValidation("--identity bot 时 --group 与批量单聊目标必须且只能指定一类")
		}
		if openID != "" || rt.Str("webhook-token") != "" {
			return apperrors.NewValidation("--identity bot 不接受 --open-dingtalk-id 或 --webhook-token")
		}
		if len(atMobiles) > 0 {
			return apperrors.NewValidation("--identity bot 不接受 --at-mobiles")
		}
		if hasDirect && (len(atOpenIDs) > 0 || len(atUserIDs) > 0) {
			return apperrors.NewValidation("bot 批量单聊不接受指定 @对象；@对象只适用于 bot 群聊")
		}
		if messagesSendIdempotencyKey(rt) != "" {
			return apperrors.NewValidation("--uuid 当前仅 user 身份的下层支持")
		}
	case "webhook":
		if rt.Str("webhook-token") == "" {
			return apperrors.NewValidation("--identity webhook 必须指定 --webhook-token")
		}
		if group != "" || openID != "" || len(users) > 0 || len(openIDs) > 0 || rt.Str("robot-code") != "" {
			return apperrors.NewValidation("--identity webhook 的目标由 token 所在群决定，不接受其他目标或 bot Code")
		}
		if len(atOpenIDs) > 0 {
			return apperrors.NewValidation("--identity webhook 不接受 --at-open-dingtalk-ids")
		}
		if messagesSendIdempotencyKey(rt) != "" {
			return apperrors.NewValidation("--uuid 当前仅 user 身份的下层支持")
		}
	}
	return nil
}

func executeMessagesSend(rt *shortcut.RuntimeContext) error {
	identity := messagesSendIdentity(rt)
	body := messagesSendBody(rt)
	title := rt.Str("title")
	if title == "" {
		title = shortcutMessageTitle(body)
	}
	switch identity {
	case "user":
		content, _ := json.Marshal(map[string]string{"title": title, "text": body})
		params := rt.AddAIMessageTag(map[string]any{
			"msgType": "markdown",
			"content": string(content),
		})
		if group := rt.StrFirst("chat-id", "group"); group != "" {
			params["openConversationId"] = group
			if values := uniqueShortcutStrings(rt.StrSlice("at-open-dingtalk-ids")); len(values) > 0 {
				params["atOpenDingTalkIds"] = values
			}
			if rt.Bool("at-all") {
				params["atAll"] = true
			}
		} else {
			params["receiverOpenDingTalkId"] = rt.Str("open-dingtalk-id")
		}
		if value := messagesSendIdempotencyKey(rt); value != "" {
			params["uuid"] = value
		}
		return executeUnifiedMessageWrite(rt, "chat", "send_personal_message", params)
	case "bot":
		params := map[string]any{
			"robotCode": rt.Str("robot-code"),
			"title":     title,
			"markdown":  body,
		}
		if group := rt.StrFirst("chat-id", "group"); group != "" {
			params["openConversationId"] = group
			if values := uniqueShortcutStrings(rt.StrSlice("at-user-ids")); len(values) > 0 {
				params["atUserIds"] = values
			}
			if values := uniqueShortcutStrings(rt.StrSlice("at-open-dingtalk-ids")); len(values) > 0 {
				params["atOpendingtalkIds"] = values
			}
			if rt.Bool("at-all") {
				params["isAtAll"] = "true"
			}
			return executeUnifiedMessageWrite(rt, "bot", "send_robot_group_message", params)
		}
		if values := uniqueShortcutStrings(rt.StrSlice("users")); len(values) > 0 {
			params["userIds"] = values
		}
		if values := uniqueShortcutStrings(rt.StrSlice("open-dingtalk-ids")); len(values) > 0 {
			params["openDingtalkIds"] = values
		}
		if rt.Bool("at-all") {
			params["isAtAll"] = "true"
		}
		return executeUnifiedMessageWrite(rt, "bot", "batch_send_robot_msg_to_users", params)
	case "webhook":
		params := map[string]any{
			"robotToken": rt.Str("webhook-token"),
			"title":      title,
			"text":       body,
		}
		if values := uniqueShortcutStrings(rt.StrSlice("at-user-ids")); len(values) > 0 {
			params["atUserIds"] = values
		}
		if values := uniqueShortcutStrings(rt.StrSlice("at-mobiles")); len(values) > 0 {
			params["atMobiles"] = values
		}
		if rt.Bool("at-all") {
			params["isAtAll"] = true
		}
		return executeUnifiedMessageWrite(rt, "bot", "send_message_by_custom_robot", params)
	default:
		return fmt.Errorf("unsupported identity %q", identity)
	}
}

func executeUnifiedMessageWrite(rt *shortcut.RuntimeContext, product, tool string, arguments map[string]any) error {
	if rt.DryRun() {
		return rt.Output(map[string]any{
			"dry_run":      true,
			"executed":     false,
			"preview_kind": "plan",
			"tool":         tool,
			"actionCount":  1,
			"failedCount":  0,
			"actions": []map[string]any{{
				"identity":  messagesSendIdentity(rt),
				"tool":      tool,
				"arguments": arguments,
			}},
		})
	}
	data, err := rt.CallMCPWriteData(product, tool, arguments)
	if err != nil {
		return err
	}
	return rt.Output(map[string]any{
		"ok":       true,
		"identity": messagesSendIdentity(rt),
		"tool":     tool,
		"result":   data,
	})
}

func messagesSendIdentity(rt *shortcut.RuntimeContext) string {
	return rt.StrFirst("as", "identity")
}

func messagesSendBody(rt *shortcut.RuntimeContext) string {
	return rt.StrFirst("markdown", "text")
}

func messagesSendIdempotencyKey(rt *shortcut.RuntimeContext) string {
	return rt.StrFirst("idempotency-key", "uuid")
}

func shortcutMessageTitle(text string) string {
	text = strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	if utf8.RuneCountInString(text) <= 40 {
		return text
	}
	runes := []rune(text)
	return string(runes[:40])
}

func init() {
	shortcut.Register(MessagesSend)
}
