package checksum

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSHA256Hex(t *testing.T) {
	// sha256("") = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	got := SHA256Hex(nil)
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	assert.Equal(t, want, got, "sha256(empty) must be the well-known empty digest")
}

func TestSHA256HexMatchesStdlib(t *testing.T) {
	data := []byte("agent state stays in the intranet")
	want := func() string {
		s := sha256.Sum256(data)
		return hex.EncodeToString(s[:])
	}()
	assert.Equal(t, want, SHA256Hex(data))
}

func TestMerkleRootEmpty(t *testing.T) {
	// empty input → sha256("") (non-zero sentinel), so two empty bundles agree
	got := MerkleRootHex(nil)
	assert.Equal(t, SHA256Hex(nil), got)
	assert.Len(t, got, 64)
}

func TestMerkleRootSingleLeaf(t *testing.T) {
	// one leaf: no pairing needed, the merkle root IS the leaf itself (Bitcoin
	// convention — a one-node tree's root is that node). This is the base case
	// the pairwise loop falls through to.
	leaf := SHA256Hex([]byte("only"))
	got := MerkleRootHex([]string{leaf})
	assert.Equal(t, leaf, got, "single-leaf merkle root = the leaf")
}

func TestMerkleRootDeterministic(t *testing.T) {
	// same set, same order (caller sorts) → same root, run twice
	digests := []string{
		SHA256Hex([]byte("a")),
		SHA256Hex([]byte("b")),
		SHA256Hex([]byte("c")),
	}
	r1 := MerkleRootHex(digests)
	r2 := MerkleRootHex(digests)
	assert.Equal(t, r1, r2)
	assert.Len(t, r1, 64)
}

func TestMerkleRootOrderSensitive(t *testing.T) {
	// the merkle construction does NOT re-sort; the caller owns ordering.
	// two different orders of the same set → different roots (proves the caller
	// must sort deterministically, which the bundle package does).
	a := SHA256Hex([]byte("a"))
	b := SHA256Hex([]byte("b"))
	assert.NotEqual(t, MerkleRootHex([]string{a, b}), MerkleRootHex([]string{b, a}))
}

func TestMerkleRootOddCountBalanced(t *testing.T) {
	// 3 leaves: odd tail duplicated at level 0 → 2 nodes → 1 root
	digests := []string{
		SHA256Hex([]byte("a")),
		SHA256Hex([]byte("b")),
		SHA256Hex([]byte("c")),
	}
	root := MerkleRootHex(digests)
	require.Len(t, root, 64)

	// tamper one leaf → different root
	tampered := make([]string, len(digests))
	copy(tampered, digests)
	tampered[1] = SHA256Hex([]byte("B"))
	assert.NotEqual(t, root, MerkleRootHex(tampered))
}

func TestVerifyHex(t *testing.T) {
	digests := []string{SHA256Hex([]byte("x")), SHA256Hex([]byte("y"))}
	root := MerkleRootHex(digests)
	assert.True(t, VerifyHex(digests, root))
	assert.False(t, VerifyHex(digests, strings.Repeat("0", 64)))
}
