// Copyright 2026 Alibaba Group
// Licensed under the Apache License, Version 2.0 (the "License");

package chat

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/helpers"
)

func TestCrossPlatformCoverageListMessageRichProjection(t *testing.T) {
	rows := listMessagesProject(map[string]any{"result": map[string]any{"messages": []any{
		map[string]any{
			"openMessageId":      "msg",
			"openConversationId": "cid",
			"threadId":           "thread",
			"msgType":            "text",
			"createTime":         "1",
			"updateTime":         "2",
			"content":            `{"mediaId":"@image"}`,
			"quotedMessage": map[string]any{
				"openMessageId": "quoted",
				"content":       "quoted",
			},
		},
	}}})
	if len(rows) != 1 {
		t.Fatalf("rows = %#v", rows)
	}
	for _, key := range []string{"threadId", "updateTime", "quotedMessage", "resourceRefs"} {
		if _, ok := rows[0][key]; !ok {
			t.Errorf("projection missing %s: %#v", key, rows[0])
		}
	}
}

func TestCrossPlatformCoverageMgetResourceDownloadOutcomes(t *testing.T) {
	baseArgs := []string{"chat", "+messages-mget", "--msg-ids", "msg", "--download-resources"}
	readyMget := `{"result":[{"openMessageId":"msg","openConversationId":"cid","content":"{\"mediaId\":\"@file\"}"}]}`
	missingContextMget := `{"result":[{"content":"{\"mediaId\":\"@file\"}"}]}`
	validInfo := `{"result":{"resourceUrl":"https://example.test/resource.bin"}}`

	t.Run("dry run", func(t *testing.T) {
		helpers.InitDeps(&larkAlignmentCaller{responses: map[string]string{
			"im/list_messages_by_ids": readyMget,
		}})
		root := newPlatformCoverageRoot()
		root.SetArgs(append(append([]string{}, baseArgs...), "--dry-run"))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("getwd", func(t *testing.T) {
		resetResourceDownloadHooks(t)
		resourceGetwd = func() (string, error) { return "", errors.New("getwd") }
		helpers.InitDeps(&larkAlignmentCaller{responses: map[string]string{
			"im/list_messages_by_ids": readyMget,
		}})
		root := newPlatformCoverageRoot()
		root.SetArgs(baseArgs)
		if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "工作目录") {
			t.Fatalf("getwd error = %v", err)
		}
	})

	cases := []struct {
		name            string
		mget            string
		info            string
		failProductTool string
		outputDir       string
		downloadErr     error
		pathErr         bool
	}{
		{name: "missing context", mget: missingContextMget, info: validInfo},
		{name: "resource lookup", mget: readyMget, failProductTool: "im/get_resource_download_url"},
		{name: "invalid info", mget: readyMget, info: `{"result":{}}`},
		{name: "path", mget: readyMget, info: validInfo, outputDir: "go.mod", pathErr: true},
		{name: "download", mget: readyMget, info: validInfo, downloadErr: errors.New("download")},
		{name: "success", mget: readyMget, info: validInfo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetResourceDownloadHooks(t)
			if tc.pathErr {
				resourceAbs = func(string) (string, error) { return "", errors.New("path") }
			}
			resourceDownload = func(
				_ context.Context,
				_ *http.Client,
				_ string,
				_ map[string]string,
				_ string,
				_ bool,
			) (int64, error) {
				return 4, tc.downloadErr
			}
			caller := &larkAlignmentCaller{
				failProductTool: tc.failProductTool,
				responses: map[string]string{
					"im/list_messages_by_ids":      tc.mget,
					"im/get_resource_download_url": tc.info,
				},
			}
			helpers.InitDeps(caller)
			root := newPlatformCoverageRoot()
			args := append([]string{}, baseArgs...)
			if tc.outputDir != "" {
				args = append(args, "--output-dir", tc.outputDir)
			}
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
