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

package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResourceDownloadInfo(t *testing.T) {
	url, headers, err := resourceDownloadInfo(map[string]any{
		"result": map[string]any{
			"resourceUrl": []any{"https://download.example.test/path/image.png"},
			"headers": map[string]any{
				"X-Test": "value",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://download.example.test/path/image.png" {
		t.Fatalf("url = %q", url)
	}
	if headers["X-Test"] != "value" {
		t.Fatalf("headers = %#v", headers)
	}
	upgraded, _, err := resourceDownloadInfo(map[string]any{
		"result": map[string]any{
			"downloadUrl": "http://bucket.oss-cn-hangzhou.aliyuncs.com/file?signature=x",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if upgraded != "https://bucket.oss-cn-hangzhou.aliyuncs.com/file?signature=x" {
		t.Fatalf("upgraded URL = %q", upgraded)
	}
	if _, _, err := resourceDownloadInfo(map[string]any{
		"result": map[string]any{"resourceUrl": "http://download.example.test/a"},
	}); err == nil {
		t.Fatal("plain HTTP URL unexpectedly accepted")
	}
}

func TestResolveResourceDownloadPath(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "downloads"), 0o755); err != nil {
		t.Fatal(err)
	}
	absolute, relative, err := resolveResourceDownloadPath(
		base,
		"downloads/",
		"https://download.example.test/path/photo.png?token=redacted",
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(absolute) != "photo.png" || filepath.ToSlash(relative) != "downloads/photo.png" {
		t.Fatalf("path = %q / %q", absolute, relative)
	}

	for _, unsafe := range []string{"../escape", filepath.Join("a", "..", "..", "escape")} {
		if _, _, err := resolveResourceDownloadPath(
			base, unsafe, "https://download.example.test/file", false,
		); err == nil {
			t.Fatalf("unsafe output %q unexpectedly accepted", unsafe)
		}
	}
}

func TestValidateResourceDownloadOutputUsesOwningFlagName(t *testing.T) {
	err := validateResourceDownloadOutputFlag("../escape", "--output-dir")
	if err == nil || !strings.Contains(err.Error(), "--output-dir") {
		t.Fatalf("error = %v, want --output-dir", err)
	}
}

func TestResolveResourceDownloadPathRejectsSymlinkParentBeforeCreatingOutside(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(base, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	_, _, err := resolveResourceDownloadPath(
		base,
		filepath.Join("linked", "new-child", "resource.bin"),
		"https://download.example.test/resource.bin",
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "符号链接") {
		t.Fatalf("symlink-parent error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "new-child")); !os.IsNotExist(statErr) {
		t.Fatalf("outside directory was created before rejection: %v", statErr)
	}
}

func TestDownloadResourceAtomically(t *testing.T) {
	const body = "verified download bytes"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	dest := filepath.Join(t.TempDir(), "resource.bin")

	size, err := downloadResourceAtomically(
		context.Background(), server.Client(), server.URL, nil, dest, false)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(body)) {
		t.Fatalf("size = %d, want %d", size, len(body))
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("body = %q", got)
	}
	if _, err := downloadResourceAtomically(
		context.Background(), server.Client(), server.URL, nil, dest, false,
	); err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("no-clobber error = %v", err)
	}
}
