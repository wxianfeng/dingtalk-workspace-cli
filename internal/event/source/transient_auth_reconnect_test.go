// Copyright 2026 Alibaba Group
// Licensed under the Apache License, Version 2.0 (the "License");

package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	authpkg "github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/auth"
	dwsevent "github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/event"
)

func transientRefreshError() error {
	return &authpkg.HTTPStatusError{StatusCode: http.StatusServiceUnavailable}
}

func terminalRefreshError() error {
	return &authpkg.HTTPStatusError{StatusCode: http.StatusUnauthorized}
}

func TestCrossPlatformCoveragePersonalRetryLogErrorReportsOnlySafeTransientStatus(t *testing.T) {
	err := retryPersonal(&accessTokenRefreshError{component: "personal source", cause: transientRefreshError()})
	if got, want := personalRetryLogError(err), "personal source: token refresh HTTP 503"; got != want {
		t.Fatalf("personalRetryLogError() = %q, want %q", got, want)
	}
}

func TestCrossPlatformCoveragePersonalSourceRetriesTransientTokenResolutionFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	src, err := NewPersonal(PersonalConfig{
		AccessTokenProvider: func(context.Context) (string, error) {
			if calls.Add(1) == 2 {
				cancel()
			}
			return "", transientRefreshError()
		},
		ClientID:     "client",
		SourceID:     "open",
		TicketURL:    "https://ticket.invalid",
		ReconnectMin: time.Millisecond,
		ReconnectMax: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = src.Start(ctx, func(*dwsevent.RawEvent) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want context canceled after retry", err)
	}
	if calls.Load() != 2 || src.State().ReconnectCount != 1 {
		t.Fatalf("provider calls=%d reconnects=%d, want 2 calls and 1 reconnect", calls.Load(), src.State().ReconnectCount)
	}
}

func TestCrossPlatformCoveragePersonalSourceDoesNotRetryTerminalTokenResolutionFailure(t *testing.T) {
	var calls atomic.Int32
	src, err := NewPersonal(PersonalConfig{
		AccessTokenProvider: func(context.Context) (string, error) {
			calls.Add(1)
			return "", terminalRefreshError()
		},
		ClientID:  "client",
		SourceID:  "open",
		TicketURL: "https://ticket.invalid",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = src.Start(context.Background(), func(*dwsevent.RawEvent) {})
	if authpkg.ClassifyRefreshFailure(err) != authpkg.RefreshFailureTerminal {
		t.Fatalf("Start() error = %v, want terminal refresh failure", err)
	}
	if calls.Load() != 1 || src.State().ReconnectCount != 0 {
		t.Fatalf("provider calls=%d reconnects=%d, want 1 call and no reconnect", calls.Load(), src.State().ReconnectCount)
	}
}

func TestCrossPlatformCoveragePersonalSourceRetriesTransientRejectedTokenRefresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ticketCalls atomic.Int32
	var refreshCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ticketCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	src, err := NewPersonal(PersonalConfig{
		AccessTokenProvider: func(context.Context) (string, error) { return "old-token", nil },
		RefreshRejectedToken: func(_ context.Context, rejected string) (string, error) {
			if rejected != "old-token" {
				t.Fatalf("rejected token = %q, want old-token", rejected)
			}
			if refreshCalls.Add(1) == 2 {
				cancel()
			}
			return "", transientRefreshError()
		},
		ClientID:     "client",
		SourceID:     "open",
		TicketURL:    srv.URL,
		HTTPClient:   srv.Client(),
		ReconnectMin: time.Millisecond,
		ReconnectMax: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = src.Start(ctx, func(*dwsevent.RawEvent) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want context canceled after retry", err)
	}
	if ticketCalls.Load() != 2 || refreshCalls.Load() != 2 || src.State().ReconnectCount != 1 {
		t.Fatalf("ticket calls=%d refresh calls=%d reconnects=%d", ticketCalls.Load(), refreshCalls.Load(), src.State().ReconnectCount)
	}
}

func TestCrossPlatformCoveragePortalSourceRetriesTransientTokenResolutionFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	src, err := New(Config{PortalTicket: &PortalTicketConfig{
		TicketURL: "https://ticket.invalid",
		AccessTokenProvider: func(context.Context) (string, error) {
			if calls.Add(1) == 2 {
				cancel()
			}
			return "", transientRefreshError()
		},
		SourceID:     "open",
		ReconnectMin: time.Millisecond,
		ReconnectMax: time.Millisecond,
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = src.Start(ctx, func(*dwsevent.RawEvent) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want context canceled after retry", err)
	}
	if calls.Load() != 2 || src.State().ReconnectCount != 1 {
		t.Fatalf("provider calls=%d reconnects=%d, want 2 calls and 1 reconnect", calls.Load(), src.State().ReconnectCount)
	}
}

func TestCrossPlatformCoveragePortalSourceDoesNotRetryTerminalTokenResolutionFailure(t *testing.T) {
	var calls atomic.Int32
	src, err := New(Config{PortalTicket: &PortalTicketConfig{
		TicketURL: "https://ticket.invalid",
		AccessTokenProvider: func(context.Context) (string, error) {
			calls.Add(1)
			return "", terminalRefreshError()
		},
		SourceID: "open",
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = src.Start(context.Background(), func(*dwsevent.RawEvent) {})
	if authpkg.ClassifyRefreshFailure(err) != authpkg.RefreshFailureTerminal {
		t.Fatalf("Start() error = %v, want terminal refresh failure", err)
	}
	if calls.Load() != 1 || src.State().ReconnectCount != 0 {
		t.Fatalf("provider calls=%d reconnects=%d, want 1 call and no reconnect", calls.Load(), src.State().ReconnectCount)
	}
}

func TestCrossPlatformCoveragePortalSourceRetriesTransientRejectedTokenRefresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ticketCalls atomic.Int32
	var refreshCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ticketCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	src, err := New(Config{PortalTicket: &PortalTicketConfig{
		TicketURL:           srv.URL,
		AccessTokenProvider: func(context.Context) (string, error) { return "old-token", nil },
		RefreshRejectedToken: func(_ context.Context, rejected string) (string, error) {
			if rejected != "old-token" {
				t.Fatalf("rejected token = %q, want old-token", rejected)
			}
			if refreshCalls.Add(1) == 2 {
				cancel()
			}
			return "", transientRefreshError()
		},
		SourceID:     "open",
		HTTPClient:   srv.Client(),
		ReconnectMin: time.Millisecond,
		ReconnectMax: time.Millisecond,
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = src.Start(ctx, func(*dwsevent.RawEvent) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want context canceled after retry", err)
	}
	if ticketCalls.Load() != 2 || refreshCalls.Load() != 2 || src.State().ReconnectCount != 1 {
		t.Fatalf("ticket calls=%d refresh calls=%d reconnects=%d", ticketCalls.Load(), refreshCalls.Load(), src.State().ReconnectCount)
	}
}
