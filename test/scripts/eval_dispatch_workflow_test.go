// Copyright 2026 Alibaba Group
// Licensed under the Apache License, Version 2.0 (the "License");

package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvalDispatchWorkflowUsesRepositoryPermissionAndReviewedSHA(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "eval-dispatch.yml"))
	if err != nil {
		t.Fatalf("read eval-dispatch workflow: %v", err)
	}
	workflow := string(data)

	for _, want := range []string{
		"/collaborators/${COMMENTER}/permission",
		"eval_dispatch_guard.py permission",
		"PR_AUTHOR: ${{ github.event.issue.user.login }}",
		"EVAL_ALLOWLIST_PATH: .github/eval-allowlist.txt",
		"REVIEWED_SHA: ${{ steps.parse.outputs.reviewed_sha }}",
		"eval_dispatch_guard.py head",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("eval-dispatch workflow missing security contract %q", want)
		}
	}
	if strings.Contains(workflow, "author_association") {
		t.Error("eval-dispatch workflow must not authorize from author_association")
	}
	if _, err := os.Stat(filepath.Join(root, ".github", "eval-allowlist.txt")); err != nil {
		t.Errorf("eval allowlist file missing on default branch: %v", err)
	}
}

func TestEvalDispatchRejectsLowRepositoryPermissions(t *testing.T) {
	t.Parallel()

	for _, permission := range []string{"read", "triage", "none"} {
		permission := permission
		t.Run(permission, func(t *testing.T) {
			t.Parallel()
			output, err := runEvalDispatchGuard(t, "permission", `{"permission":"`+permission+`"}`, "COMMENTER=low-privilege-user")
			if err == nil {
				t.Fatalf("permission %q unexpectedly allowed; output=%s", permission, output)
			}
			if !strings.Contains(output, "does not have write, maintain, or admin permission") {
				t.Fatalf("permission %q rejection = %q, want trusted-permission error", permission, output)
			}
		})
	}
}

func TestEvalDispatchAllowsTrustedRepositoryPermissions(t *testing.T) {
	t.Parallel()

	for _, permission := range []string{"write", "maintain", "admin"} {
		permission := permission
		t.Run(permission, func(t *testing.T) {
			t.Parallel()
			output, err := runEvalDispatchGuard(t, "permission", `{"permission":"`+permission+`"}`, "COMMENTER=maintainer")
			if err != nil {
				t.Fatalf("permission %q rejected: %v\n%s", permission, err, output)
			}
		})
	}
}

func TestEvalDispatchRejectsChangedPRHead(t *testing.T) {
	t.Parallel()

	const reviewedSHA = "1111111111111111111111111111111111111111"
	const currentSHA = "2222222222222222222222222222222222222222"
	pr := `{"number":934,"state":"open","head":{"sha":"` + currentSHA + `"}}`
	output, err := runEvalDispatchGuard(
		t,
		"head",
		pr,
		"EXPECTED_PR_NUMBER=934",
		"REVIEWED_SHA="+reviewedSHA,
	)
	if err == nil {
		t.Fatalf("changed PR head unexpectedly allowed; output=%s", output)
	}
	if !strings.Contains(output, "PR head changed after review") {
		t.Fatalf("changed-head rejection = %q, want explicit stale-review error", output)
	}
}

func TestEvalDispatchAcceptsReviewedCurrentPRHead(t *testing.T) {
	t.Parallel()

	const reviewedSHA = "1111111111111111111111111111111111111111"
	pr := `{"number":934,"state":"open","head":{"sha":"` + reviewedSHA + `"}}`
	output, err := runEvalDispatchGuard(
		t,
		"head",
		pr,
		"EXPECTED_PR_NUMBER=934",
		"REVIEWED_SHA="+reviewedSHA,
	)
	if err != nil {
		t.Fatalf("reviewed current PR head rejected: %v\n%s", err, output)
	}
	if !strings.Contains(output, "head_sha="+reviewedSHA) {
		t.Fatalf("head guard output = %q, want pinned head SHA", output)
	}
}

func TestEvalCommentRequiresExplicitReviewedSHA(t *testing.T) {
	t.Parallel()

	const reviewedSHA = "1111111111111111111111111111111111111111"
	output, err := runEvalCommentParser(t, "/eval drive sha="+reviewedSHA)
	if err != nil {
		t.Fatalf("explicit reviewed SHA rejected: %v\n%s", err, output)
	}
	for _, want := range []string{"products=drive", "reviewed_sha=" + reviewedSHA} {
		if !strings.Contains(output, want) {
			t.Errorf("parser output = %q, want %q", output, want)
		}
	}

	// sha 省略在解析层放行（空 reviewed_sha），由 guard 按评论者是否 PR 作者裁决
	output, err = runEvalCommentParser(t, "/eval drive")
	if err != nil {
		t.Fatalf("omitted reviewed SHA rejected at parse layer: %v\n%s", err, output)
	}
	if !strings.Contains(output, "reviewed_sha=\n") && !strings.HasSuffix(output, "reviewed_sha=") {
		t.Fatalf("parser output = %q, want empty reviewed_sha passthrough", output)
	}
}

func TestEvalDispatchAutoPinsOwnPRHeadWhenShaOmitted(t *testing.T) {
	t.Parallel()

	const currentSHA = "3333333333333333333333333333333333333333"
	pr := `{"number":934,"state":"open","user":{"login":"Internal-Contributor"},"head":{"sha":"` + currentSHA + `"}}`
	output, err := runEvalDispatchGuard(
		t,
		"head",
		pr,
		"EXPECTED_PR_NUMBER=934",
		"REVIEWED_SHA=",
		"COMMENTER=internal-contributor",
	)
	if err != nil {
		t.Fatalf("own-PR auto pin rejected: %v\n%s", err, output)
	}
	if !strings.Contains(output, "head_sha="+currentSHA) {
		t.Fatalf("guard output = %q, want auto-pinned current head", output)
	}
}

func TestEvalDispatchRequiresShaForOthersPRs(t *testing.T) {
	t.Parallel()

	const currentSHA = "3333333333333333333333333333333333333333"
	pr := `{"number":934,"state":"open","user":{"login":"someone-else"},"head":{"sha":"` + currentSHA + `"}}`
	output, err := runEvalDispatchGuard(
		t,
		"head",
		pr,
		"EXPECTED_PR_NUMBER=934",
		"REVIEWED_SHA=",
		"COMMENTER=internal-contributor",
	)
	if err == nil {
		t.Fatalf("other author's PR without sha unexpectedly allowed; output=%s", output)
	}
	if !strings.Contains(output, "requires an explicit reviewed sha") {
		t.Fatalf("missing-sha rejection = %q, want explicit-sha requirement", output)
	}
}

func TestEvalDispatchAllowsAllowlistedAuthorOnOwnPR(t *testing.T) {
	t.Parallel()

	allowlist := writeEvalAllowlist(t, "# 自助评测名单\nInternal-Contributor  # 内部贡献者\n")
	output, err := runEvalDispatchGuard(
		t,
		"permission",
		`{"message":"Not Found"}`,
		"COMMENTER=internal-contributor",
		"PR_AUTHOR=Internal-Contributor",
		"EVAL_ALLOWLIST_PATH="+allowlist,
	)
	if err != nil {
		t.Fatalf("allowlisted author rejected on own PR: %v\n%s", err, output)
	}
	if !strings.Contains(output, "authorized=allowlisted-author") {
		t.Fatalf("guard output = %q, want allowlisted-author authorization", output)
	}
}

func TestEvalDispatchRejectsAllowlistedUserOnOthersPR(t *testing.T) {
	t.Parallel()

	allowlist := writeEvalAllowlist(t, "internal-contributor\n")
	output, err := runEvalDispatchGuard(
		t,
		"permission",
		`{"message":"Not Found"}`,
		"COMMENTER=internal-contributor",
		"PR_AUTHOR=someone-else",
		"EVAL_ALLOWLIST_PATH="+allowlist,
	)
	if err == nil {
		t.Fatalf("allowlisted user unexpectedly dispatched another author's PR; output=%s", output)
	}
	if !strings.Contains(output, "self-service only") {
		t.Fatalf("cross-PR rejection = %q, want self-service-only error", output)
	}
}

func TestEvalDispatchRejectsUnlistedCommenterWithoutWrite(t *testing.T) {
	t.Parallel()

	allowlist := writeEvalAllowlist(t, "# 空名单\n")
	output, err := runEvalDispatchGuard(
		t,
		"permission",
		`{"permission":"read"}`,
		"COMMENTER=stranger",
		"PR_AUTHOR=stranger",
		"EVAL_ALLOWLIST_PATH="+allowlist,
	)
	if err == nil {
		t.Fatalf("unlisted commenter unexpectedly allowed; output=%s", output)
	}
	if !strings.Contains(output, "not an allowlisted PR author") {
		t.Fatalf("unlisted rejection = %q, want allowlist-aware error", output)
	}
}

func writeEvalAllowlist(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "eval-allowlist.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write allowlist fixture: %v", err)
	}
	return path
}

func runEvalDispatchGuard(t *testing.T, mode, input string, env ...string) (string, error) {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	cmd := exec.Command("python3", filepath.Join(root, "scripts", "ci", "eval_dispatch_guard.py"), mode)
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = append(os.Environ(), env...)
	output, runErr := cmd.CombinedOutput()
	return string(output), runErr
}

func runEvalCommentParser(t *testing.T, comment string) (string, error) {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	outputPath := filepath.Join(t.TempDir(), "github-output")
	cmd := exec.Command("python3", filepath.Join(root, "scripts", "ci", "eval_comment_parse.py"))
	cmd.Env = append(
		os.Environ(),
		"COMMENT_BODY="+comment,
		"GITHUB_OUTPUT="+outputPath,
	)
	combined, runErr := cmd.CombinedOutput()
	fileOutput, readErr := os.ReadFile(outputPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read parser GitHub output: %v", readErr)
	}
	return string(combined) + string(fileOutput), runErr
}
