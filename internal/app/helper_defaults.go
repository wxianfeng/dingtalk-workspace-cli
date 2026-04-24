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

package app

import (
	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/compat"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// walkLeafPairs descends through envelope (dyn) and helper subtrees in
// lockstep, invoking registerHelperDefaultsForPair only on same-named
// leaf/leaf pairs. Group/group paths recurse; any shape mismatch is a
// terminating condition (cmdutil.MergeHardcodedLeaves will warn and keep
// the dynamic side authoritative — helper defaults for that subtree are
// not registered because there's no envelope leaf to inject them into).
//
// Design intent: this is a dry walk. It reads both sides but mutates only
// the compat routeRegistry via compat.AddHelperDefault. It runs before
// cmdutil.MergeHardcodedLeaves so the helper subtree is still intact
// (post-merge, helper-only subtrees are grafted onto dyn and the helper
// root loses its original child set).
func walkLeafPairs(dyn, helper *cobra.Command) {
	if dyn == nil || helper == nil {
		return
	}
	// Both sides are leaves → register whatever helper defaults apply.
	dynIsLeaf := !dyn.HasSubCommands()
	helperIsLeaf := !helper.HasSubCommands()
	if dynIsLeaf && helperIsLeaf {
		registerHelperDefaultsForPair(dyn, helper)
		return
	}
	// One side is a leaf and the other a group — shape mismatch; the
	// authoritative side (dyn) stays, MergeHardcodedLeaves emits a warn,
	// and we simply skip helper-default registration for this branch.
	if dynIsLeaf != helperIsLeaf {
		return
	}
	// Both groups → recurse on same-named children.
	for _, hc := range helper.Commands() {
		if hc == nil {
			continue
		}
		name := hc.Name()
		if name == "" {
			continue
		}
		dc, _, err := dyn.Find([]string{name})
		if err != nil || dc == nil || dc == dyn || dc.Name() != name {
			// Helper-only subtree — cmdutil.MergeHardcodedLeaves will
			// graft it later; no envelope leaf exists to register
			// defaults against, so skip.
			continue
		}
		walkLeafPairs(dc, hc)
	}
}

// registerHelperDefaultsForPair inspects every flag on a hardcoded helper
// leaf and, when the cobra Flag.DefValue is considered a meaningful
// default, registers it as a fallback on the envelope-sourced leaf via
// compat.AddHelperDefault.
//
// Skip conditions (each a purposeful backwards-compat defense):
//   - envelope leaf doesn't have a same-named flag. We cannot inject a
//     parameter the envelope tool doesn't even know about — the MCP
//     backend would reject unknown keys.
//   - helper Flag.DefValue is "" (no default at all).
//   - helper flag is a bool whose DefValue is "false". pflag cannot
//     distinguish "explicitly defaulted to false" from "zero value", so
//     injecting the false would change MCP payload shape for every call.
//     The envelope envelope already carries bool defaults where it needs
//     them; helper bool=false is treated as "no opinion".
//
// Intentionally NOT skipped here: "envelope flag already declares a
// default". That check cannot be inferred from cobra alone because
// numeric/bool flags always carry a non-empty DefValue (e.g. "0" for
// int, "false" for bool) even when the envelope did not declare one —
// pflag fills in the Kind's zero value. The authoritative source of
// truth is compat.routeDefaults.envelopeClaimed, populated from
// FlagBinding.Default at NewDirectCommand time, and
// compat.AddHelperDefault refuses registration when that set contains
// the Property. Delegating the decision there keeps a single source of
// truth across the two packages.
func registerHelperDefaultsForPair(envLeaf, helperLeaf *cobra.Command) {
	if envLeaf == nil || helperLeaf == nil {
		return
	}
	helperLeaf.Flags().VisitAll(func(f *pflag.Flag) {
		if f == nil {
			return
		}
		if f.DefValue == "" {
			return
		}
		if isBoolFalseZeroValue(f) {
			return
		}
		if envLeaf.Flags().Lookup(f.Name) == nil {
			return
		}
		compat.AddHelperDefault(envLeaf, f.Name, f.DefValue)
	})
}

// isBoolFalseZeroValue reports whether f is a bool flag whose current
// default is pflag's zero value ("false"). See rationale on
// registerHelperDefaultsForPair.
func isBoolFalseZeroValue(f *pflag.Flag) bool {
	if f == nil || f.Value == nil {
		return false
	}
	if f.Value.Type() != "bool" {
		return false
	}
	return f.DefValue == "false"
}
