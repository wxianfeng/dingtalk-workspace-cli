// Copyright 2026 Alibaba Group
// Licensed under the Apache License, Version 2.0

package upgrade

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseVersionFromBackupName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"v1.0.6-20260401-093000", "1.0.6"},
		{"v0.2.7-20260314-100523", "0.2.7"},
		{"v1.0.0", "1.0.0"},
		{"invalid", "unknown"},
		{"v", "unknown"},
	}
	for _, tt := range tests {
		got := parseVersionFromBackupName(tt.name)
		if got != tt.want {
			t.Errorf("parseVersionFromBackupName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestRollbackManagerBackupAndList(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManagerWithDir(dir)

	// Create a fake binary to backup
	fakeExe := filepath.Join(dir, "dws")
	os.WriteFile(fakeExe, []byte("#!/bin/sh\necho fake"), 0755)

	// ListBackups on empty dir
	backups, err := rm.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups() error = %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("ListBackups() on empty = %d, want 0", len(backups))
	}
}

func TestRollbackManagerCleanup(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManagerWithDir(dir)

	// Create backup directories with proper timestamps in info.json
	names := []string{"v1.0.1-20260101-010000", "v1.0.2-20260102-020000", "v1.0.3-20260103-030000"}
	for i, name := range names {
		backupDir := filepath.Join(dir, name)
		os.MkdirAll(backupDir, 0755)
		info := BackupInfo{
			Path:      backupDir,
			Version:   "1.0." + string(rune('1'+i)),
			CreatedAt: mustParseTime(t, "2026-01-0"+string(rune('1'+i))+"T01:00:00Z"),
		}
		rm.saveBackupInfo(info)
	}

	// Keep only 1 (the newest)
	if err := rm.Cleanup(1); err != nil {
		t.Fatalf("Cleanup(1) error = %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("after Cleanup(1), %d entries remain, want 1", len(entries))
	}
}

func TestRollbackManagerCleanup_KeepAll(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManagerWithDir(dir)

	for i := 0; i < 3; i++ {
		name := "v1.0." + string(rune('0'+i)) + "-20260101-01000" + string(rune('0'+i))
		backupDir := filepath.Join(dir, name)
		os.MkdirAll(backupDir, 0755)
		rm.saveBackupInfo(BackupInfo{
			Path:      backupDir,
			Version:   "1.0." + string(rune('0'+i)),
			CreatedAt: mustParseTime(t, "2026-01-0"+string(rune('1'+i))+"T01:00:00Z"),
		})
	}

	if err := rm.Cleanup(10); err != nil {
		t.Fatalf("Cleanup(10) error = %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Errorf("after Cleanup(10), %d entries remain, want 3", len(entries))
	}
}

func TestRollbackManagerCleanup_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManagerWithDir(dir)
	if err := rm.Cleanup(1); err != nil {
		t.Fatalf("Cleanup on empty dir error = %v", err)
	}
}

func TestListBackups_WithInfoJSON(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManagerWithDir(dir)

	backupDir := filepath.Join(dir, "v1.0.5-20260315-120000")
	os.MkdirAll(filepath.Join(backupDir, "binary"), 0755)
	os.WriteFile(filepath.Join(backupDir, "binary", "dws"), []byte("#!/bin/sh"), 0755)

	info := BackupInfo{
		Path:       backupDir,
		BinaryPath: filepath.Join(backupDir, "binary", "dws"),
		Version:    "1.0.5",
		CreatedAt:  mustParseTime(t, "2026-03-15T12:00:00Z"),
		Size:       1234,
	}
	rm.saveBackupInfo(info)

	backups, err := rm.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups() error = %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("ListBackups() len = %d, want 1", len(backups))
	}
	if backups[0].Version != "1.0.5" {
		t.Errorf("Version = %q, want %q", backups[0].Version, "1.0.5")
	}
	if backups[0].Size != 1234 {
		t.Errorf("Size = %d, want 1234", backups[0].Size)
	}
	if backups[0].BinaryPath == "" {
		t.Error("BinaryPath should be set from info.json")
	}
}

func TestListBackups_WithoutInfoJSON(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManagerWithDir(dir)

	// Create a backup dir without info.json => should fallback to name parsing
	backupDir := filepath.Join(dir, "v1.0.3-20260101-100000")
	os.MkdirAll(backupDir, 0755)

	backups, err := rm.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups() error = %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("len = %d, want 1", len(backups))
	}
	if backups[0].Version != "1.0.3" {
		t.Errorf("fallback Version = %q, want 1.0.3", backups[0].Version)
	}
}

func TestListBackups_SortOrder(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManagerWithDir(dir)

	timestamps := []string{
		"2026-01-01T01:00:00Z",
		"2026-03-15T12:00:00Z",
		"2026-02-10T06:00:00Z",
	}
	for i, ts := range timestamps {
		name := "v1.0." + string(rune('0'+i)) + "-backup"
		backupDir := filepath.Join(dir, name)
		os.MkdirAll(backupDir, 0755)
		rm.saveBackupInfo(BackupInfo{
			Path:      backupDir,
			Version:   "1.0." + string(rune('0'+i)),
			CreatedAt: mustParseTime(t, ts),
		})
	}

	backups, err := rm.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 3 {
		t.Fatalf("len = %d", len(backups))
	}
	// Should be sorted newest first
	if backups[0].Version != "1.0.1" {
		t.Errorf("newest = %q, want 1.0.1 (March)", backups[0].Version)
	}
	if backups[2].Version != "1.0.0" {
		t.Errorf("oldest = %q, want 1.0.0 (January)", backups[2].Version)
	}
}

func TestListBackups_NonexistentDir(t *testing.T) {
	rm := NewRollbackManagerWithDir("/tmp/nonexistent-dir-" + time.Now().Format("20060102150405"))
	backups, err := rm.ListBackups()
	if err != nil {
		t.Fatalf("error = %v (should return nil for nonexistent dir)", err)
	}
	if backups != nil {
		t.Errorf("should return nil for nonexistent dir, got %d entries", len(backups))
	}
}

func TestListBackups_SkipsRegularFiles(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManagerWithDir(dir)

	// Create a regular file (not a directory) — should be ignored
	os.WriteFile(filepath.Join(dir, "not-a-backup.txt"), []byte("ignore"), 0644)

	// Create an actual backup directory
	backupDir := filepath.Join(dir, "v1.0.0-20260101-000000")
	os.MkdirAll(backupDir, 0755)
	rm.saveBackupInfo(BackupInfo{
		Path:      backupDir,
		Version:   "1.0.0",
		CreatedAt: mustParseTime(t, "2026-01-01T00:00:00Z"),
	})

	backups, err := rm.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Errorf("len = %d, want 1 (file should be ignored)", len(backups))
	}
}

func TestSaveAndLoadBackupInfo(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManagerWithDir(dir)

	backupDir := filepath.Join(dir, "v1.0.0-test")
	os.MkdirAll(backupDir, 0755)

	original := BackupInfo{
		Path:       backupDir,
		BinaryPath: filepath.Join(backupDir, "binary", "dws"),
		Version:    "1.0.0",
		CreatedAt:  mustParseTime(t, "2026-06-01T10:00:00Z"),
		Size:       5678,
	}
	rm.saveBackupInfo(original)

	loaded, err := rm.loadBackupInfo(backupDir)
	if err != nil {
		t.Fatalf("loadBackupInfo() error = %v", err)
	}
	if loaded.Version != original.Version {
		t.Errorf("Version = %q, want %q", loaded.Version, original.Version)
	}
	if loaded.Size != original.Size {
		t.Errorf("Size = %d, want %d", loaded.Size, original.Size)
	}
	if loaded.BinaryPath != original.BinaryPath {
		t.Errorf("BinaryPath = %q, want %q", loaded.BinaryPath, original.BinaryPath)
	}
}

func TestLoadBackupInfo_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	rm := NewRollbackManagerWithDir(dir)

	backupDir := filepath.Join(dir, "v1.0.0-bad")
	os.MkdirAll(backupDir, 0755)
	os.WriteFile(filepath.Join(backupDir, "info.json"), []byte("{bad json"), 0644)

	_, err := rm.loadBackupInfo(backupDir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestNewRollbackManager(t *testing.T) {
	rm := NewRollbackManager()
	if rm.backupDir == "" {
		t.Error("backupDir should not be empty")
	}
	if rm.maxBackups != defaultMaxBackups {
		t.Errorf("maxBackups = %d, want %d", rm.maxBackups, defaultMaxBackups)
	}
}

func TestCopyDir(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "file1.txt"), []byte("content1"), 0644)
	os.MkdirAll(filepath.Join(src, "sub"), 0755)
	os.WriteFile(filepath.Join(src, "sub", "file2.txt"), []byte("content2"), 0644)

	dst := filepath.Join(t.TempDir(), "copy")

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir() error = %v", err)
	}

	got1, _ := os.ReadFile(filepath.Join(dst, "file1.txt"))
	if string(got1) != "content1" {
		t.Errorf("file1 = %q, want content1", string(got1))
	}
	got2, _ := os.ReadFile(filepath.Join(dst, "sub", "file2.txt"))
	if string(got2) != "content2" {
		t.Errorf("sub/file2 = %q, want content2", string(got2))
	}
}

func TestCopyDir_EmptySrc(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "copy")

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir() error = %v", err)
	}

	entries, _ := os.ReadDir(dst)
	if len(entries) != 0 {
		t.Errorf("dst should be empty, got %d entries", len(entries))
	}
}

func TestCopyDir_SrcNotExist(t *testing.T) {
	err := copyDir("/tmp/nonexistent-src-"+time.Now().Format("150405"), t.TempDir())
	if err == nil {
		t.Error("expected error for nonexistent src")
	}
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}
