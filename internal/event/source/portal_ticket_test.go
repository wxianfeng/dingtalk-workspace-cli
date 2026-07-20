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

package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestPortalTicket401RejectedTokenRefreshRetry(t *testing.T) {
	var ticketCalls atomic.Int32
	var forceRefreshCalls atomic.Int32
	currentToken := "old-token"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(ticketCalls.Add(1))
		if call == 1 {
			// First call: return 401
			if got := r.Header.Get("x-user-access-token"); got != "old-token" {
				t.Errorf("first request token = %q, want %q", got, "old-token")
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Second call: verify new token is used
		if got := r.Header.Get("x-user-access-token"); got != "new-token" {
			t.Errorf("second request token = %q, want %q", got, "new-token")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": map[string]any{
				"endpoint": "wss://example.test/connect",
				"ticket":   "ticket-refreshed",
			},
		})
	}))
	defer srv.Close()

	cfg := &PortalTicketConfig{
		TicketURL:   srv.URL,
		AccessToken: "static-fallback",
		AccessTokenProvider: func(context.Context) (string, error) {
			return currentToken, nil
		},
		RefreshRejectedToken: func(_ context.Context, rejected string) (string, error) {
			if rejected != "old-token" {
				t.Fatalf("rejected token = %q, want old-token", rejected)
			}
			forceRefreshCalls.Add(1)
			currentToken = "new-token"
			return "new-token", nil
		},
		SourceID:   "pre_open_source",
		Mode:       "normal",
		HTTPClient: srv.Client(),
	}

	// First call: should get 401 and refresh the rejected token.
	_, err := requestPortalTicket(context.Background(), cfg)
	if err == nil {
		t.Fatal("first requestPortalTicket() should return error on 401")
	}
	if got := forceRefreshCalls.Load(); got != 1 {
		t.Fatalf("RefreshRejectedToken called %d times, want 1", got)
	}

	// Second call (simulating outer-layer retry): should succeed with new token
	ticket, err := requestPortalTicket(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second requestPortalTicket() error = %v", err)
	}
	if ticket.Endpoint == "" || ticket.Ticket == "" {
		t.Fatalf("ticket = %#v, want non-empty endpoint and ticket", ticket)
	}
	if got := ticketCalls.Load(); got != 2 {
		t.Fatalf("ticket HTTP calls = %d, want 2", got)
	}
}
