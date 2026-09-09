// Package main — CLI-level tests for the pack/verify/unpack surface.
//
// These exercise the cobra commands end-to-end against temp fixture state
// dirs: the structural-validation gate on verify/unpack (m5), the tool-version
// lockstep between --version, the VERSION file and manifest.tool_version
// (m6/m7), and the plain happy path.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCLI executes the root command with args and returns stdout, stderr, and
// the execution error (a failed subcommand returns its RunE error).
func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errBuf.String(), err
}

// writeCLIFixture builds a minimal ~/.claude state dir with one session and a
// global memory; returns its path.
func writeCLIFixture(t *testing.T) string {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), ".claude")
	projDir := filepath.Join(stateDir, "projects", "-tmp-projA")
	require.NoError(t, os.MkdirAll(projDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(projDir, "s1.jsonl"),
		[]byte("{\"type\":\"user\",\"message\":\"hello\"}\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(stateDir, "CLAUDE.md"), []byte("# Global\nUse Go.\n"), 0o644))
	return stateDir
}

// tamperBundle rewrites one nested manifest field in a .csb file and saves it
// to a new path (checksums are left valid; only the manifest field changes).
func tamperBundle(t *testing.T, inPath, field, value, outPath string) {
	t.Helper()
	raw, err := os.ReadFile(inPath)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	manifest := doc["manifest"].(map[string]any)
	manifest[field] = value
	data, err := json.MarshalIndent(doc, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(outPath, append(data, '\n'), 0o644))
}

func TestCLIPackVerifyHappyPath(t *testing.T) {
	tmp := t.TempDir()
	stateDir := writeCLIFixture(t)
	csb := filepath.Join(tmp, "project.csb")

	out, _, err := runCLI(t, "pack", "--harness", "claudecode", "--state-dir", stateDir, "--out", csb)
	require.NoError(t, err)
	assert.Contains(t, out, "packed")

	// the audit sidecar is part of the pack contract
	sidecar, err := os.ReadFile(csb + ".manifest.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(sidecar), "source_harness: claudecode")
	assert.Contains(t, string(sidecar), "tool_version: carrystate-"+Version)

	out, _, err = runCLI(t, "verify", "--bundle", csb)
	require.NoError(t, err)
	assert.Contains(t, out, "bundle is intact")
}

// v0.1.0 regression: a bundle with an unsupported manifest.spec_version passed
// verify because the structural validation was never invoked.
func TestCLIVerifyRejectsTamperedSpecVersion(t *testing.T) {
	tmp := t.TempDir()
	stateDir := writeCLIFixture(t)
	csb := filepath.Join(tmp, "project.csb")
	_, _, err := runCLI(t, "pack", "--harness", "claudecode", "--state-dir", stateDir, "--out", csb)
	require.NoError(t, err)

	bad := filepath.Join(tmp, "bad-spec.csb")
	tamperBundle(t, csb, "spec_version", "9.9", bad)

	_, _, err = runCLI(t, "verify", "--bundle", bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

// unpack must refuse a schema-incompatible bundle before writing anything.
func TestCLIUnpackRefusesSchemaIncompatible(t *testing.T) {
	tmp := t.TempDir()
	stateDir := writeCLIFixture(t)
	csb := filepath.Join(tmp, "project.csb")
	_, _, err := runCLI(t, "pack", "--harness", "claudecode", "--state-dir", stateDir, "--out", csb)
	require.NoError(t, err)

	bad := filepath.Join(tmp, "bad-schema.csb")
	raw, err := os.ReadFile(csb)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	manifest := doc["manifest"].(map[string]any)
	schemaMap := manifest["schema_map"].(map[string]any)
	schemaMap["sessions"] = "csb-session-jsonl-v2"
	data, err := json.MarshalIndent(doc, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(bad, append(data, '\n'), 0o644))

	target := filepath.Join(tmp, "codex")
	_, _, err = runCLI(t, "unpack", "--harness", "codex", "--in", bad, "--state-dir", target)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema incompatible")

	// refused before any write: the target state dir was never created
	_, statErr := os.Stat(target)
	assert.True(t, os.IsNotExist(statErr), "target dir must not exist after refusal")
}

// --version, the repo VERSION file, and manifest.tool_version must all agree.
func TestCLIVersionLockstep(t *testing.T) {
	out, _, err := runCLI(t, "--version")
	require.NoError(t, err)
	assert.Contains(t, out, Version, "--version prints the Version const")

	versionFile, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	require.NoError(t, err)
	assert.Equal(t, Version, strings.TrimSpace(string(versionFile)),
		"VERSION file matches the Version const")

	tmp := t.TempDir()
	stateDir := writeCLIFixture(t)
	csb := filepath.Join(tmp, "project.csb")
	_, _, err = runCLI(t, "pack", "--harness", "claudecode", "--state-dir", stateDir, "--out", csb)
	require.NoError(t, err)

	raw, err := os.ReadFile(csb)
	require.NoError(t, err)
	var doc struct {
		Manifest struct {
			ToolVersion string `json:"tool_version"`
		} `json:"manifest"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, "carrystate-"+Version, doc.Manifest.ToolVersion,
		"manifest.tool_version follows the binary version")
}
