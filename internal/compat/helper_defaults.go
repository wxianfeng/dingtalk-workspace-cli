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

// Package compat — helper default fallback channel.
//
// Envelope-declared flag defaults (flagOverride.Default) are the primary
// source of MCP payload defaults. When the envelope omits a default, the
// open-source helper command for the same leaf may still carry a cobra
// Flag.DefValue that callers (chiefly internal/app.mergeDynamicWithHelpers)
// can register as a fallback via AddHelperDefault.
//
// Priority order, all sharing the same "already in params → skip" rule:
//
//  1. envelope defaultInjects       (flagOverride.Default)
//  2. envelope envDefaults          (flagOverride.EnvDefault)
//  3. envelope runtimeDefaults      (flagOverride.RuntimeDefault)
//  4. helper defaults               (this file; AddHelperDefault)
//
// Envelope always wins. Once the envelope declares a Default for a parameter
// the helper fallback for the same parameter is refused at registration
// time (AddHelperDefault returns false). The fallback therefore silently
// no-ops as soon as the server side fills in the missing default, which is
// the backwards-compatibility contract between this mechanism and future
// Diamond envelope updates.

package compat

import (
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

// helperDefaultEntry mirrors defaultInjectEntry in dynamic_commands.go.
// Duplicated here to avoid exporting an internal type; the normalizer
// loop treats both in the same switch via writeDefaultByKind.
type helperDefaultEntry struct {
	paramName    string
	defaultValue string
	kind         ValueKind
}

// routeDefaults is the per-command record maintained by the registry.
// Indexed by *cobra.Command pointer because the cobra.Command type does
// not expose a user-data slot and Annotations are string-typed.
type routeDefaults struct {
	mu sync.Mutex

	// bindings snapshots Route.Bindings at NewDirectCommand time. Used by
	// AddHelperDefault to look up the FlagBinding (and thus Property/Kind)
	// for a given flag name (main, alias, or extra alias).
	bindings []FlagBinding

	// envelopeClaimed is the set of Property names whose envelope Default
	// was non-empty at build time. AddHelperDefault refuses to register
	// a helper default for any claimed property.
	envelopeClaimed map[string]bool

	// helpers is the appended-at-runtime slice of helper defaults. Order
	// is preserved so duplicate AddHelperDefault calls are idempotent in
	// "first registration wins" fashion.
	helpers []helperDefaultEntry
}

// routeRegistry is a package-local registry keyed by the envelope-sourced
// *cobra.Command. Cobra command instances outlive their registration
// callers for the lifetime of the app; GC pressure is not a concern
// because the command tree is long-lived by construction.
var routeRegistry sync.Map

// registerRouteForHelperDefaults records the bindings for cmd so that
// AddHelperDefault can resolve flag names to bindings and so the
// envelope-Default set can be built once up-front.
func registerRouteForHelperDefaults(cmd *cobra.Command, bindings []FlagBinding) {
	if cmd == nil {
		return
	}
	claimed := make(map[string]bool, len(bindings))
	for _, b := range bindings {
		if strings.TrimSpace(b.Default) == "" {
			continue
		}
		prop := strings.TrimSpace(b.Property)
		if prop == "" {
			continue
		}
		claimed[prop] = true
	}
	routeRegistry.Store(cmd, &routeDefaults{
		bindings:        append([]FlagBinding(nil), bindings...),
		envelopeClaimed: claimed,
	})
}

// AddHelperDefault registers a fallback default value for flagName on cmd.
// The value is applied to MCP params at RunE time if and only if the
// envelope itself did not declare a Default for the same Property and the
// user did not change the corresponding flag.
//
// Returns true on success. Returns false and no-ops when:
//   - cmd is nil or was not produced by NewDirectCommand (and therefore
//     has no registered bindings).
//   - flagName does not map to any known binding (primary FlagName, Alias,
//     or extra Aliases) on cmd.
//   - defaultValue is empty (nothing to inject).
//   - the matched Property is already in envelopeClaimed (envelope declared
//     a Default itself).
//   - the matched Property already has a helper default registered (first
//     registration wins; idempotent for repeated app-layer walks).
func AddHelperDefault(cmd *cobra.Command, flagName, defaultValue string) bool {
	if cmd == nil || strings.TrimSpace(flagName) == "" || defaultValue == "" {
		return false
	}
	v, ok := routeRegistry.Load(cmd)
	if !ok {
		return false
	}
	rd, ok := v.(*routeDefaults)
	if !ok || rd == nil {
		return false
	}

	binding, found := lookupBindingByFlagName(rd.bindings, flagName)
	if !found {
		return false
	}
	prop := strings.TrimSpace(binding.Property)
	if prop == "" {
		return false
	}

	rd.mu.Lock()
	defer rd.mu.Unlock()

	if rd.envelopeClaimed[prop] {
		return false
	}
	for _, existing := range rd.helpers {
		if existing.paramName == prop {
			return false
		}
	}
	rd.helpers = append(rd.helpers, helperDefaultEntry{
		paramName:    prop,
		defaultValue: defaultValue,
		kind:         binding.Kind,
	})
	return true
}

// helperDefaultsFor returns a snapshot of helper defaults registered for
// cmd. Returns nil when cmd was not produced by NewDirectCommand. The
// returned slice is a fresh copy; callers may iterate without locking.
func helperDefaultsFor(cmd *cobra.Command) []helperDefaultEntry {
	if cmd == nil {
		return nil
	}
	v, ok := routeRegistry.Load(cmd)
	if !ok {
		return nil
	}
	rd, ok := v.(*routeDefaults)
	if !ok || rd == nil {
		return nil
	}
	rd.mu.Lock()
	defer rd.mu.Unlock()
	if len(rd.helpers) == 0 {
		return nil
	}
	out := make([]helperDefaultEntry, len(rd.helpers))
	copy(out, rd.helpers)
	return out
}

// lookupBindingByFlagName matches flagName against the primary FlagName,
// Alias, and extra Aliases of each binding, in that precedence order
// (mirroring firstChangedFlag). Returns the first matching binding.
func lookupBindingByFlagName(bindings []FlagBinding, flagName string) (FlagBinding, bool) {
	name := strings.TrimSpace(flagName)
	if name == "" {
		return FlagBinding{}, false
	}
	for _, b := range bindings {
		if strings.TrimSpace(b.FlagName) == name {
			return b, true
		}
		if strings.TrimSpace(b.Alias) == name {
			return b, true
		}
		for _, a := range b.Aliases {
			if strings.TrimSpace(a) == name {
				return b, true
			}
		}
	}
	return FlagBinding{}, false
}

// resetHelperDefaultsRegistryForTest clears the registry. Only intended
// for use by tests that span goroutines or repeatedly rebuild the same
// cmd pointer; production callers never drain the registry.
func resetHelperDefaultsRegistryForTest() {
	routeRegistry.Range(func(k, _ any) bool {
		routeRegistry.Delete(k)
		return true
	})
}

// HelperDefaultsSnapshotForTesting returns the currently registered helper
// default fallbacks for cmd as a name→value map. Kind is intentionally
// omitted because tests asserting the fallback chain exercise it end-to-end
// through the normalizer (see TestNormalizer_DefaultChainOrder); callers
// that only need to verify registration shape use this snapshot.
//
// Exported solely as a cross-package test seam — internal/app's merge tests
// cannot reach the unexported helperDefaultsFor. Do not use from production
// code; the snapshot intentionally drops kind information to discourage
// mistaking this for the runtime injection path.
func HelperDefaultsSnapshotForTesting(cmd *cobra.Command) map[string]string {
	entries := helperDefaultsFor(cmd)
	if len(entries) == 0 {
		return nil
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		out[e.paramName] = e.defaultValue
	}
	return out
}

// RegisterRouteForHelperDefaultsForTesting is an exported alias for the
// unexported registration function, so cross-package tests (internal/app)
// can set up a bare cmd without going through NewDirectCommand. Production
// callers always go through NewDirectCommand.
func RegisterRouteForHelperDefaultsForTesting(cmd *cobra.Command, bindings []FlagBinding) {
	registerRouteForHelperDefaults(cmd, bindings)
}
