package workspace

import (
	"strings"
	"unicode"
)

// Stats describes the size and shape of a draft.
//
// Counting happens here rather than in the browser so the number in the
// editor, the number in the post list and the number used anywhere else
// all come from one implementation and cannot drift apart.
type Stats struct {
	Words      int `json:"words"`      // CJK characters plus Latin words
	Characters int `json:"characters"` // every rune, spaces included
	NoSpaces   int `json:"noSpaces"`   // characters excluding whitespace
	CJK        int `json:"cjk"`
	Latin      int `json:"latin"` // Latin/other-script words
	Paragraphs int `json:"paragraphs"`
	Sentences  int `json:"sentences"`
	// ReadMinutes is how long this takes to read aloud, rounded up, so a
	// short draft reads as 1 rather than 0.
	ReadMinutes int `json:"readMinutes"`
}

// readingSpeed is words per minute. Chinese prose is usually quoted around
// 300–500 characters a minute; 400 sits in the middle and is close enough
// to English reading speed that a mixed document does not skew.
const readingSpeed = 400

// Analyze measures a markdown body.
//
// Markup is measured as written: stripping it would make the count jump
// around while someone is mid-syntax, which is worse than counting a few
// stray characters.
func Analyze(body string) Stats {
	var s Stats
	inWord := false

	for _, r := range body {
		s.Characters++
		if !unicode.IsSpace(r) {
			s.NoSpaces++
		}

		switch {
		case isCJK(r):
			s.CJK++
			inWord = false
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			if !inWord {
				s.Latin++
				inWord = true
			}
		default:
			inWord = false
		}

		if isSentenceEnd(r) {
			s.Sentences++
		}
	}

	s.Words = s.CJK + s.Latin

	for _, block := range strings.Split(body, "\n\n") {
		if strings.TrimSpace(block) != "" {
			s.Paragraphs++
		}
	}

	if s.Words > 0 {
		s.ReadMinutes = (s.Words + readingSpeed - 1) / readingSpeed
	}
	return s
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

// isSentenceEnd covers both the ASCII terminators and their full-width
// counterparts, which is what Chinese text actually uses.
func isSentenceEnd(r rune) bool {
	switch r {
	case '.', '!', '?', '。', '！', '？', '…':
		return true
	}
	return false
}

// WordCount reports just the word total, which the index stores.
func WordCount(body string) int { return Analyze(body).Words }
