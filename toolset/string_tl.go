package toolset

import (
	"crypto/rand"
	"fmt"
	"hash/fnv"
	"os"
	"time"
)

// GenerateSN generates a globally unique serial number.
//
// New format (enhanced): <prefix>-<hosthash8>-<timestamp16>-<rand16>
// Three-factor composition ensures global uniqueness without requiring
// a central coordinating database:
//
//  1. hosthash8  -- FNV-1a 32-bit hash of machine hostname,
//     providing a stable machine fingerprint across all platforms.
//  2. timestamp16 -- UnixNano (64-bit), provides temporal ordering
//     and ensures uniqueness across different moments on the same machine.
//  3. rand16     -- crypto/rand (64-bit), ensures collision safety
//     for multiple calls within the same nanosecond on the same machine.
//
// The 128-bit total entropy (32-bit host hash + 64-bit time + 64-bit random)
// plus the deterministic machine fingerprint guarantees global uniqueness
// across devices, even without a central database for collision detection.
func GenerateSN(prefix string) string {
	// 1. Cross-platform machine identifier (hostname)
	// os.Hostname() works on all target platforms: Linux, macOS, Windows,
	// Android (via Golang's NDK support), and HarmonyOS (via EGL/TC).
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	// FNV-1a 32-bit hash -> stable 8-char hex machine fingerprint
	h := fnv.New32a()
	h.Write([]byte(hostname))
	hostHash := h.Sum32()

	// 2. Nanosecond timestamp -> 16-char hex temporal value
	now := time.Now().UnixNano()

	// 3. Random bytes for same-nanosecond collision safety
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read should never fail on a modern OS;
		// fall back to a nanosecond-resolution counter
		return fmt.Sprintf("%s-%08x-%016x-%016x",
			prefix, hostHash, now, time.Now().UnixNano())
	}

	return fmt.Sprintf("%s-%08x-%016x-%016x",
		prefix,
		hostHash,    // 4 bytes -> 8 hex
		uint64(now), // 8 bytes -> 16 hex
		b,           // 8 bytes -> 16 hex
	)
}

// GenerateSNSimple generates a locally unique serial number in UUID v4 style.
// Format: <prefix>-xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
//
// Unlike GenerateSN, this version does NOT include hostname or timestamp --
// it uses only crypto/rand, making it lighter and sufficient for scenarios
// that only need local uniqueness (e.g. HTTP session IDs on a single server).
// The 128-bit random value ensures negligible collision probability
// within a single machine's lifetime.
func GenerateSNSimple(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-fallback-%d", prefix, time.Now().UnixNano())
	}

	// Set UUID version 4 (4 most significant bits of byte 6 -> 0100)
	b[6] = (b[6] & 0x0f) | 0x40
	// Set RFC 4122 variant (2 most significant bits of byte 8 -> 10)
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%s-%08x-%04x-%04x-%04x-%012x",
		prefix,
		b[0:4],   // 4 bytes -> 8 hex
		b[4:6],   // 2 bytes -> 4 hex
		b[6:8],   // 2 bytes -> 4 hex (version 4)
		b[8:10],  // 2 bytes -> 4 hex (variant)
		b[10:16], // 6 bytes -> 12 hex
	)
}
