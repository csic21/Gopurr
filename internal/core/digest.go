package core

import (
	"crypto/sha256"
	"encoding/hex"
)

// ConfigDigest identifies the exact application payload last accepted by a
// daemon. It is runtime evidence, never an inference from a file on disk.
func ConfigDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
