package toolset

// IsCJK reports whether r is a CJK (Chinese/Japanese/Korean) character.
func IsCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Unified Ideographs Extension A
		(r >= 0x2E80 && r <= 0x2EFF) || // CJK Radicals Supplement
		(r >= 0x3000 && r <= 0x303F) || // CJK Symbols and Punctuation
		(r >= 0x2F00 && r <= 0x2FDF) || // Kangxi Radicals
		(r >= 0x31C0 && r <= 0x31EF) || // CJK Strokes
		(r >= 0x3200 && r <= 0x32FF) || // Enclosed CJK Letters and Months
		(r >= 0x3300 && r <= 0x33FF) || // CJK Compatibility
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility Ideographs
		(r >= 0xFE30 && r <= 0xFE4F) || // CJK Compatibility Forms
		(r >= 0x20000 && r <= 0x2FFFF) || // CJK Unified Ideographs Extension B+
		(r >= 0x30000 && r <= 0x3FFFF) // CJK Unified Ideographs Extension G+
}

// IsWhitespace reports whether r is a whitespace character.
func IsWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v'
}

// IsLetter reports whether r is an English letter (both uppercase and lowercase).
func IsLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// IsEmoji roughly checks whether r is an emoji or special symbol.
// Covers common emoji ranges and some special symbols.
func IsEmoji(r rune) bool {
	return (r >= 0x1F300 && r <= 0x1F9FF) || // Miscellaneous Symbols, Pictographs, and Emoticons
		(r >= 0x2600 && r <= 0x26FF) || // Miscellaneous Symbols
		(r >= 0x2700 && r <= 0x27BF) || // Dingbats
		(r >= 0xFE00 && r <= 0xFE0F) || // Variation Selectors
		(r >= 0x1F600 && r <= 0x1F64F) || // Emoticons
		(r >= 0x1F680 && r <= 0x1F6FF) || // Transport and Map Symbols
		(r >= 0x1F1E0 && r <= 0x1F1FF) || // Regional Indicator Symbols
		(r >= 0x200D) // Zero Width Joiner (ZWJ, used for combining emoji)
}

// TruncateTitle truncates a string to at most maxLen runes for use as a session title.
// It also collapses whitespace/newlines into a single space.
// If the string exceeds maxLen, it appends "…" at the end.
func TruncateTitle(s string, maxLen int) string {
	// Collapse whitespace and newlines
	runes := []rune(s)
	var result []rune
	space := false
	for _, r := range runes {
		switch r {
		case '\n', '\r', '\t', ' ':
			if !space {
				result = append(result, ' ')
				space = true
			}
		default:
			result = append(result, r)
			space = false
		}
	}
	trimmed := string(result)
	// Limit to maxLen characters
	runes2 := []rune(trimmed)
	if len(runes2) > maxLen {
		return string(runes2[:maxLen]) + "…"
	}
	return trimmed
}
