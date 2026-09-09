// Package bundle — schema validation + compatibility helpers (the
// `internal/bundle/schema.go` polish-stage file).
//
// These functions are the programmatic surface of the .csb spec (the
// human-readable spec is SPEC.md). An adapter calls SchemaCompatibleForImport
// before writing a bundle to a target harness, and `carrystate verify` calls
// Validate to surface structural problems (missing manifest, unknown spec
// version, entry without a checksum) before the per-entry sha256 comparison.
package bundle

import (
	"fmt"
	"strings"
)

// SupportedSpecVersions is the set of .csb schema versions this binary can
// read + verify. v0.1/v0.2 binaries support only "0.1"; a future "0.2" bundle
// would require a translator step (see SPEC.md §versioning) before this
// binary can verify it.
var SupportedSpecVersions = map[string]bool{
	"0.1": true,
}

// Validate checks a bundle for structural problems: a known spec version, a
// non-empty source harness, and that every entry has a non-empty sha256. It
// does NOT re-derive checksums — use Bundle.Verify for that. Returns nil if
// the structure is sound, or a multi-line error listing every issue found.
func (b *Bundle) Validate() error {
	var problems []string

	if b.Manifest.SpecVersion == "" {
		problems = append(problems, "manifest.spec_version is empty (not a .csb file?)")
	} else if !SupportedSpecVersions[b.Manifest.SpecVersion] {
		problems = append(problems, fmt.Sprintf(
			"manifest.spec_version %q is not supported (this binary supports: %s)",
			b.Manifest.SpecVersion, strings.Join(supportedVersionsList(), ", ")))
	}

	if b.Manifest.SourceHarness == "" {
		problems = append(problems, "manifest.source_harness is empty")
	}

	if b.Manifest.CreatedAt.IsZero() {
		problems = append(problems, "manifest.created_at is zero")
	}

	for i, e := range b.Sessions {
		if e.ID == "" {
			problems = append(problems, fmt.Sprintf("sessions[%d].id is empty", i))
		}
		if e.SHA256 == "" {
			problems = append(problems, fmt.Sprintf("sessions[%s].sha256 is empty", e.ID))
		}
		if e.Content == "" {
			problems = append(problems, fmt.Sprintf("sessions[%s].content is empty", e.ID))
		}
	}
	for i, e := range b.Memories {
		if e.ID == "" {
			problems = append(problems, fmt.Sprintf("memories[%d].id is empty", i))
		}
		if e.SHA256 == "" {
			problems = append(problems, fmt.Sprintf("memories[%s].sha256 is empty", e.ID))
		}
		if e.Content == "" {
			problems = append(problems, fmt.Sprintf("memories[%s].content is empty", e.ID))
		}
	}
	for i, s := range b.MCP.Servers {
		if s.Name == "" {
			problems = append(problems, fmt.Sprintf("mcp.servers[%d].name is empty", i))
		}
		if s.SHA256 == "" {
			problems = append(problems, fmt.Sprintf("mcp.servers[%s].sha256 is empty", s.Name))
		}
	}

	if b.Checksums.BundleMerkleRoot == "" {
		problems = append(problems, "checksums.bundle_merkle_root is empty")
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("bundle validation failed:\n  - %s", strings.Join(problems, "\n  - "))
}

// SchemaCompatibleForImport reports whether a target adapter that understands
// the given session/memory/mcp schema ids can import this bundle without a
// translator upgrade. v0.1 adapters all use the "csb-*-v1" schema ids, so any
// v0.1 bundle is compatible with any v0.1 adapter; a v0.2 bundle with new
// schema ids would fail here until a translator is registered.
func (b *Bundle) SchemaCompatibleForImport(sessionSchema, memorySchema, mcpSchema string) error {
	mism := []string{}
	if b.Manifest.SchemaMap.Sessions != "" && b.Manifest.SchemaMap.Sessions != sessionSchema {
		mism = append(mism, fmt.Sprintf("sessions: bundle %q vs target %q", b.Manifest.SchemaMap.Sessions, sessionSchema))
	}
	if b.Manifest.SchemaMap.Memories != "" && b.Manifest.SchemaMap.Memories != memorySchema {
		mism = append(mism, fmt.Sprintf("memories: bundle %q vs target %q", b.Manifest.SchemaMap.Memories, memorySchema))
	}
	if b.Manifest.SchemaMap.MCP != "" && b.Manifest.SchemaMap.MCP != mcpSchema {
		mism = append(mism, fmt.Sprintf("mcp: bundle %q vs target %q", b.Manifest.SchemaMap.MCP, mcpSchema))
	}
	if len(mism) == 0 {
		return nil
	}
	return fmt.Errorf("schema incompatible (install a translator upgrade for v0.2 bundles):\n  - %s", strings.Join(mism, "\n  - "))
}

// supportedVersionsList returns the sorted list of supported spec versions, for
// error messages.
func supportedVersionsList() []string {
	out := make([]string, 0, len(SupportedSpecVersions))
	for v := range SupportedSpecVersions {
		out = append(out, v)
	}
	// stable order: "0.1" < "0.2" lexically for these short version strings
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
