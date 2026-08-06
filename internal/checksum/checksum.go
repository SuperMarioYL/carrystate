// Package checksum provides the sha256 + merkle-root primitives the CarryState
// Bundle uses for content-addressing. Every bundle entry is sha256-addressed by
// its canonical content; the bundle root is a merkle hash over the sorted
// per-entry digests, so a single 32-byte value attests to the whole bundle.
//
// The functions here are deliberately stdlib-only (crypto/sha256 + encoding/hex)
// so the verifier has zero non-stdlib surface — important for a tool that lives
// or dies by "airgapped, auditable, reproducible".
package checksum

import (
	"crypto/sha256"
	"encoding/hex"
)

// SHA256Hex returns the lowercase hex sha256 of data.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// MerkleRootHex folds a list of hex sha256 digests into a single hex merkle
// root. Empty input → sha256("") (a well-defined, non-zero value, so an empty
// bundle still has a stable root rather than the sentinel "" that two
// different empty bundles could disagree on).
//
// Construction (Bitcoin-style):
//   - decode each hex digest to 32 bytes
//   - pairwise: child = sha256(left || right)
//   - odd tail: duplicate the last node so the tree stays balanced
//   - repeat until one node remains
//
// The caller is expected to sort the input deterministically (the bundle
// package sorts by entry id before calling); this function does not re-sort, so
// two callers with the same set but different order would get different roots —
// which is why the bundle package owns the ordering contract.
func MerkleRootHex(digests []string) string {
	if len(digests) == 0 {
		return SHA256Hex(nil)
	}

	// decode hex → 32-byte leaves; skip any that fail to decode (defensive; the
	// bundle package always produces valid hex)
	layer := make([][]byte, 0, len(digests))
	for _, h := range digests {
		b, err := hex.DecodeString(h)
		if err != nil || len(b) != sha256.Size {
			b = []byte(h) // fall back to hashing the raw string bytes
		}
		layer = append(layer, b)
	}

	for len(layer) > 1 {
		next := make([][]byte, 0, (len(layer)+1)/2)
		for i := 0; i < len(layer); i += 2 {
			left := layer[i]
			var right []byte
			if i+1 < len(layer) {
				right = layer[i+1]
			} else {
				right = layer[i] // duplicate last on odd tail
			}
			pair := make([]byte, 0, len(left)+len(right))
			pair = append(pair, left...)
			pair = append(pair, right...)
			sum := sha256.Sum256(pair)
			next = append(next, sum[:])
		}
		layer = next
	}
	return hex.EncodeToString(layer[0])
}

// VerifyHex recomputes the merkle root over digests and compares to expected.
func VerifyHex(digests []string, expected string) bool {
	return MerkleRootHex(digests) == expected
}
