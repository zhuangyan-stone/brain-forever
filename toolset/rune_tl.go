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

// IsLetter 判断 rune 是否为英文字母（包括大小写）。
func IsLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// IsEmoji 粗略判断 rune 是否为 emoji 或特殊符号。
// 覆盖常见的 emoji 区间和一些特殊符号。
func IsEmoji(r rune) bool {
	return (r >= 0x1F300 && r <= 0x1F9FF) || // 杂项符号和 pictograph、表情符号
		(r >= 0x2600 && r <= 0x26FF) || // 杂项符号
		(r >= 0x2700 && r <= 0x27BF) || // 装饰符号
		(r >= 0xFE00 && r <= 0xFE0F) || // 变体选择器
		(r >= 0x1F600 && r <= 0x1F64F) || // 表情符号
		(r >= 0x1F680 && r <= 0x1F6FF) || // 交通和地图符号
		(r >= 0x1F1E0 && r <= 0x1F1FF) || // 区域指示符
		(r >= 0x200D) // 零宽连接符（ZWJ，用于组合 emoji）
}
