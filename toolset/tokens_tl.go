package toolset

// ============================================================
// Token Estimation Utilities
//
// This file provides functions to estimate the number of tokens
// a given text will consume. The estimation logic is based on
// common tokenization characteristics shared by major Chinese LLM
// providers (DeepSeek, Qwen, GLM, Baichuan, Yi, etc.):
//
//   - CJK characters: ~1.5-2.5 tokens each, using 2.0 as typical
//   - English letters/words: BPE-style, ~1 token per 4 letters
//   - Digits: ~1 token per 3 consecutive digits
//   - Whitespace: usually not tokenized independently, attached to neighbors
//   - Punctuation: ~0.5-1 token each, using 0.8 as typical
//   - Emoji/special symbols: ~2-4 tokens each, using 3.0 as typical
//
// The estimation weights reflect common patterns across these models:
//   - DeepSeek V2/V3 (deepseek-chat, deepseek-reasoner)
//   - Qwen2.5 (Tongyi Qianwen)
//   - GLM-4 (Zhipu AI)
//   - Baichuan4
//   - Yi (01.AI)
//   - Moonshot (Dark Side of the Moon)
//   - Minimax
//
// Note: These are estimates. Actual token counts depend on the
// specific model's tokenizer implementation.
// ============================================================

// tokenKind classifies a rune or rune-run for token estimation purposes.
type tokenKind int

const (
	tkindCJK     tokenKind = iota // CJK character
	tkindDigit                    // consecutive digits
	tkindSpace                    // consecutive spaces/tabs
	tkindNewline                  // consecutive newlines
	tkindLetter                   // consecutive English letters
	tkindEmoji                    // emoji / special symbol
	tkindPunct                    // punctuation / other symbol
)

// tokenSpan describes a classified span of text and its estimated token value.
type tokenSpan struct {
	kind  tokenKind
	count int     // number of runes in this span
	value float64 // estimated token contribution
}

// TokenEstimate returns the estimated number of tokens for the given text.
func TokenEstimate(text string) int {
	if text == "" {
		return 0
	}
	var total float64
	walkTokens(text, func(s tokenSpan) {
		total += s.value
	})
	result := int(total + 0.999)
	if result < 1 {
		return 1
	}
	return result
}

// TokenEstimateDetailed returns a detailed breakdown of the estimated token count.
func TokenEstimateDetailed(text string) TokenDetail {
	if text == "" {
		return TokenDetail{}
	}
	var d TokenDetail
	walkTokens(text, func(s tokenSpan) {
		d.TotalF += s.value
		switch s.kind {
		case tkindCJK:
			d.CJKCount += s.count
			d.CJKTokens += s.value
		case tkindLetter:
			d.LetterCount += s.count
			d.LetterTokens += s.value
		case tkindDigit:
			d.DigitCount += s.count
			d.DigitTokens += s.value
		case tkindSpace:
			d.SpaceCount += s.count
			d.SpaceTokens += s.value
		case tkindNewline:
			d.NewlineCount += s.count
			d.NewlineTokens += s.value
		case tkindPunct:
			d.PunctCount += s.count
			d.PunctTokens += s.value
		case tkindEmoji:
			d.EmojiCount += s.count
			d.EmojiTokens += s.value
		}
	})
	d.TotalTokens = int(d.TotalF + 0.999)
	if d.TotalTokens < 1 {
		d.TotalTokens = 1
	}
	return d
}

// walkTokens iterates through the text and calls fn for each classified span.
//
// This is the shared traversal core used by both TokenEstimate and
// TokenEstimateDetailed.
func walkTokens(text string, fn func(tokenSpan)) {
	runes := []rune(text)
	n := len(runes)

	i := 0
	for i < n {
		r := runes[i]

		switch {
		case IsCJK(r):
			fn(tokenSpan{kind: tkindCJK, count: 1, value: 2.0})
			i++

		case r >= '0' && r <= '9':
			j := i
			for j < n && runes[j] >= '0' && runes[j] <= '9' {
				j++
			}
			c := j - i
			fn(tokenSpan{kind: tkindDigit, count: c, value: float64(c) / 3.0})
			i = j

		case r == ' ' || r == '\t':
			j := i
			for j < n && (runes[j] == ' ' || runes[j] == '\t') {
				j++
			}
			c := j - i
			v := 0.0
			if c >= 4 {
				v = float64(c) / 4.0
			}
			fn(tokenSpan{kind: tkindSpace, count: c, value: v})
			i = j

		case r == '\n' || r == '\r':
			j := i
			for j < n && (runes[j] == '\n' || runes[j] == '\r') {
				j++
			}
			c := j - i
			v := 0.0
			if c >= 2 {
				v = float64(c) / 2.0
			}
			fn(tokenSpan{kind: tkindNewline, count: c, value: v})
			i = j

		case IsLetter(r):
			j := i
			for j < n && IsLetter(runes[j]) {
				j++
			}
			c := j - i
			fn(tokenSpan{kind: tkindLetter, count: c, value: float64(c) / 3.5})
			i = j

		case IsEmoji(r):
			fn(tokenSpan{kind: tkindEmoji, count: 1, value: 3.0})
			i++

		default:
			fn(tokenSpan{kind: tkindPunct, count: 1, value: 0.8})
			i++
		}
	}
}

// TokenDetail stores a detailed breakdown of estimated token counts.
type TokenDetail struct {
	TotalTokens int     // Total estimated token count
	TotalF      float64 // internal: accumulated float total

	CJKCount  int     // Number of CJK characters
	CJKTokens float64 // Estimated tokens from CJK characters

	LetterCount  int     // Number of English letters
	LetterTokens float64 // Estimated tokens from English letters

	DigitCount  int     // Number of digit characters
	DigitTokens float64 // Estimated tokens from digits

	SpaceCount  int     // Number of space/tab characters
	SpaceTokens float64 // Estimated tokens from spaces/tabs

	NewlineCount  int     // Number of newline characters
	NewlineTokens float64 // Estimated tokens from newlines

	PunctCount  int     // Number of punctuation/symbol characters
	PunctTokens float64 // Estimated tokens from punctuation/symbols

	EmojiCount  int     // Number of emoji characters
	EmojiTokens float64 // Estimated tokens from emoji
}
