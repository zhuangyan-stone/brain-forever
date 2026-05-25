package toolset

import (
	"crypto/rand"
	"fmt"
	"time"
)

// GenerateSN generates a globally unique serial number in UUID v4 style.
// Format: <prefix>-xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
// Where x is random hex, 4 is UUID version 4, y is RFC 4122 variant (8/a/b/9).
// The 128-bit random value (crypto/rand) ensures global uniqueness.
func GenerateSN(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read should never fail on a modern OS;
		// fall back to a less secure but still functional ID
		return fmt.Sprintf("%s-fallback-%d", prefix, time.Now().UnixNano())
	}

	// Set UUID version 4 (4 most significant bits of byte 6 → 0100)
	b[6] = (b[6] & 0x0f) | 0x40
	// Set RFC 4122 variant (2 most significant bits of byte 8 → 10)
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%s-%08x-%04x-%04x-%04x-%012x",
		prefix,
		b[0:4],   // 4 bytes → 8 hex
		b[4:6],   // 2 bytes → 4 hex
		b[6:8],   // 2 bytes → 4 hex (version 4)
		b[8:10],  // 2 bytes → 4 hex (variant)
		b[10:16], // 6 bytes → 12 hex
	)
}
