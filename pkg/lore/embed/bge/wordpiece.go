package bge

import (
	"fmt"
	"strings"
	"unicode"
)

// wordPieceTokenizer is a pure-Go port of the subset of HuggingFace
// BertTokenizer used by BAAI/bge-small-en-v1.5. Configuration is
// hard-coded to match bge-small's tokenizer_config.json:
//
//	do_lower_case=true
//	tokenize_chinese_chars=true
//	strip_accents=None
//	do_basic_tokenize=true
type wordPieceTokenizer struct {
	vocab        map[string]int
	unkToken     string
	clsToken     string
	sepToken     string
	padToken     string
	maxInputChar int
}

// newWordPieceTokenizer constructs a tokenizer with bge-small defaults.
func newWordPieceTokenizer(vocab map[string]int) *wordPieceTokenizer {
	return &wordPieceTokenizer{
		vocab:        vocab,
		unkToken:     "[UNK]",
		clsToken:     "[CLS]",
		sepToken:     "[SEP]",
		padToken:     "[PAD]",
		maxInputChar: 100,
	}
}

// assertVocabHasSpecials returns an error if any required special token is
// absent from the vocabulary. Called at construction so missing tokens fail
// fast rather than silently producing [UNK] at inference time.
func (t *wordPieceTokenizer) assertVocabHasSpecials() error {
	for _, s := range []string{t.clsToken, t.sepToken, t.padToken, t.unkToken} {
		if _, ok := t.vocab[s]; !ok {
			return fmt.Errorf("embed: vocab missing required special token %q", s)
		}
	}
	return nil
}

// encode runs BasicTokenizer + WordPiece, prepends [CLS] and appends [SEP],
// and truncates to maxLen (inclusive of specials). Returns token ids,
// attention mask (all ones for real tokens), and token_type_ids (all zeros
// for single-segment input).
func (t *wordPieceTokenizer) encode(text string, maxLen int) (ids, mask, typeIDs []int64) {
	pieces := t.tokenize(text)
	cls := int64(t.vocab[t.clsToken])
	sep := int64(t.vocab[t.sepToken])

	ids = append(ids, cls)
	for _, p := range pieces {
		if id, ok := t.vocab[p]; ok {
			ids = append(ids, int64(id))
		} else {
			ids = append(ids, int64(t.vocab[t.unkToken]))
		}
	}
	ids = append(ids, sep)

	if maxLen > 0 && len(ids) > maxLen {
		ids = ids[:maxLen]
		ids[maxLen-1] = sep
	}

	mask = make([]int64, len(ids))
	typeIDs = make([]int64, len(ids))
	for i := range ids {
		mask[i] = 1
	}
	return ids, mask, typeIDs
}

// tokenize runs BasicTokenizer then WordPiece over each produced token.
func (t *wordPieceTokenizer) tokenize(text string) []string {
	basics := basicTokenize(text)
	var out []string
	for _, b := range basics {
		out = append(out, t.wordpiece(b)...)
	}
	return out
}

// basicTokenize is a Go port of BertTokenizer.BasicTokenizer.tokenize with
// do_lower_case=true, tokenize_chinese_chars=true, strip_accents=None.
func basicTokenize(text string) []string {
	text = cleanText(text)
	text = tokenizeCJK(text)
	whiteSpace := strings.Fields(text)
	var out []string
	for _, tok := range whiteSpace {
		tok = strings.ToLower(tok)
		out = append(out, splitOnPunc(tok)...)
	}
	var joined []string
	for _, o := range out {
		for _, s := range strings.Fields(o) {
			if s != "" {
				joined = append(joined, s)
			}
		}
	}
	return joined
}

// cleanText removes control characters and normalizes whitespace to a single
// ASCII space.
func cleanText(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if r == 0 || r == 0xfffd || isControl(r) {
			continue
		}
		if isWhitespace(r) {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// tokenizeCJK adds spaces around CJK ideographs so they become standalone
// tokens.
func tokenizeCJK(text string) string {
	var b strings.Builder
	b.Grow(len(text) + 8)
	for _, r := range text {
		if isCJK(r) {
			b.WriteRune(' ')
			b.WriteRune(r)
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// splitOnPunc splits a token on punctuation characters, emitting each
// punctuation character as its own token.
func splitOnPunc(text string) []string {
	var out []string
	var cur []rune
	for _, r := range text {
		if isPunctuation(r) {
			if len(cur) > 0 {
				out = append(out, string(cur))
				cur = cur[:0]
			}
			out = append(out, string(r))
			continue
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

// wordpiece implements greedy longest-match from the left; subword tokens
// are prefixed "##". Returns [UNK] for tokens that cannot be segmented or
// exceed maxInputChar runes.
func (t *wordPieceTokenizer) wordpiece(token string) []string {
	runes := []rune(token)
	if len(runes) > t.maxInputChar {
		return []string{t.unkToken}
	}
	var pieces []string
	start := 0
	for start < len(runes) {
		end := len(runes)
		var matched string
		for end > start {
			sub := string(runes[start:end])
			if start > 0 {
				sub = "##" + sub
			}
			if _, ok := t.vocab[sub]; ok {
				matched = sub
				break
			}
			end--
		}
		if matched == "" {
			return []string{t.unkToken}
		}
		pieces = append(pieces, matched)
		start = end
	}
	return pieces
}

func isWhitespace(r rune) bool {
	if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
		return true
	}
	return unicode.IsSpace(r)
}

func isControl(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	return unicode.IsControl(r)
}

func isPunctuation(r rune) bool {
	switch {
	case r >= 33 && r <= 47:
		return true
	case r >= 58 && r <= 64:
		return true
	case r >= 91 && r <= 96:
		return true
	case r >= 123 && r <= 126:
		return true
	}
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func isCJK(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF:
		return true
	case r >= 0x3400 && r <= 0x4DBF:
		return true
	case r >= 0x20000 && r <= 0x2A6DF:
		return true
	case r >= 0x2A700 && r <= 0x2B73F:
		return true
	case r >= 0x2B740 && r <= 0x2B81F:
		return true
	case r >= 0x2B820 && r <= 0x2CEAF:
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0x2F800 && r <= 0x2FA1F:
		return true
	}
	return false
}
