// SPDX-License-Identifier: MIT

package asposepdf

import (
	"encoding/hex"
	"strings"
)

// parseCMap parses a ToUnicode CMap stream and returns a mapping
// from character codes (glyph IDs) to Unicode runes.
// It handles beginbfchar/endbfchar and beginbfrange/endbfrange sections.
//
// LOCAL PATCH, doc-anonymiser. Upstream v0.7.0 splits the stream into LINES and
// arms a section only on a line ENDING with "beginbfchar"/"beginbfrange",
// disarming only on a line that IS exactly "endbfchar"/"endbfrange". A CMap is a
// PostScript-style token program in which a newline is ordinary whitespace, so a
// producer may legally emit the whole program on one line. Microsoft Print To
// PDF does exactly that, and against such a file the line reader never arms,
// returns an EMPTY map, and every glyph of a /Type0 /Identity-H font extracts as
// U+FFFD: thousands of characters, not one of them a letter.
//
// This version scans TOKENS, which is what the format is. It also removes two
// smaller consequences of the line assumption: more than one code/value pair on
// one line is no longer truncated to the first, and a bfrange entry whose
// operands straddle a newline is no longer dropped.
//
// This file is the ONE patched file in vendor/. Remove the patch when upstream
// ships the fix, and delete the guard test that pins it (docs/TESTING.md).
func parseCMap(data []byte) map[uint16]rune {
	m := make(map[uint16]rune)

	// Whitespace separates tokens and means nothing else, so every flavour of
	// line ending collapses to a plain space before the scan.
	replacer := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ")
	fields := strings.Fields(replacer.Replace(string(data)))

	const (
		outside = iota
		inBfchar
		inBfrange
	)
	section := outside

	// operands collects the current section's tokens, applied when the section
	// ENDS rather than as they arrive: a bfrange entry is three tokens (or two
	// plus an array), and what separates one entry from the next is the count
	// the section declares, never a line break.
	var operands []string

	apply := func() {
		switch section {
		case inBfchar:
			applyBfchars(operands, m)
		case inBfrange:
			applyBfranges(operands, m)
		}
		operands = nil
	}

	for _, f := range fields {
		switch {
		case f == "beginbfchar":
			apply()
			section, operands = inBfchar, nil
		case f == "beginbfrange":
			apply()
			section, operands = inBfrange, nil
		case f == "endbfchar", f == "endbfrange":
			apply()
			section = outside
		case section == outside:
			// Header material: the codespace ranges, /CIDSystemInfo, the
			// /CMapName, and the counts that precede each section.
		default:
			// A bracket may be glued to a hex operand ("[<0041>", "<0043>]"),
			// because a CMap is free to write its arrays with or without
			// surrounding whitespace. Brackets are kept as their own markers so
			// the array form stays recognisable in applyBfranges.
			operands = append(operands, splitCMapToken(f)...)
		}
	}
	// A malformed CMap can end without its closing token; what was collected
	// so far is still usable, and dropping it would lose the whole font.
	apply()
	return m
}

// applyBfchars maps each <code> <unicode> pair.
func applyBfchars(operands []string, m map[uint16]rune) {
	for i := 0; i+1 < len(operands); i += 2 {
		if operands[i] == "[" || operands[i] == "]" {
			continue
		}
		if r := decodeHexRune(operands[i+1]); r != 0 {
			m[decodeHexUint16(operands[i])] = r
		}
	}
}

// applyBfranges maps each <lo> <hi> <unicodeStart> triple, and each
// <lo> <hi> [<u> <u> ...] array form.
func applyBfranges(operands []string, m map[uint16]rune) {
	for i := 0; i+2 < len(operands); {
		lo := decodeHexUint16(operands[i])
		hi := decodeHexUint16(operands[i+1])

		if operands[i+2] == "[" {
			j := i + 3
			for k := 0; j < len(operands) && operands[j] != "]"; j, k = j+1, k+1 {
				code := lo + uint16(k)
				if code > hi {
					continue
				}
				if r := decodeHexRune(operands[j]); r != 0 {
					m[code] = r
				}
			}
			i = j + 1
			continue
		}

		if dst := decodeHexRune(operands[i+2]); dst != 0 && hi >= lo {
			for c := uint32(lo); c <= uint32(hi); c++ {
				m[uint16(c)] = dst + rune(c-uint32(lo))
			}
		}
		i += 3
	}
}

// splitCMapToken separates array brackets from a hex operand glued to them, so
// "[<0041>" becomes "[" then "<0041>". Anything that is not a bracket and not a
// <hex> token is dropped: inside a section there is nothing else to read.
func splitCMapToken(f string) []string {
	var out []string
	for strings.HasPrefix(f, "[") {
		out = append(out, "[")
		f = f[1:]
	}
	var trailing []string
	for strings.HasSuffix(f, "]") {
		trailing = append(trailing, "]")
		f = f[:len(f)-1]
	}
	out = append(out, extractHexTokens(f)...)
	return append(out, trailing...)
}

// extractHexTokens returns all <hex> tokens from a string.
func extractHexTokens(s string) []string {
	var tokens []string
	for {
		start := strings.IndexByte(s, '<')
		if start < 0 {
			break
		}
		end := strings.IndexByte(s[start:], '>')
		if end < 0 {
			break
		}
		tokens = append(tokens, s[start+1:start+end])
		s = s[start+end+1:]
	}
	return tokens
}

// decodeHexUint16 decodes a hex string to uint16 (e.g., "0003" -> 3).
func decodeHexUint16(s string) uint16 {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) == 0 {
		return 0
	}
	if len(b) == 1 {
		return uint16(b[0])
	}
	return uint16(b[0])<<8 | uint16(b[1])
}

// decodeHexRune decodes a hex string to a rune (e.g., "0041" -> 'A').
// Supplementary-plane targets (>2 bytes) are not supported and return 0.
func decodeHexRune(s string) rune {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) == 0 || len(b) > 2 {
		return 0
	}
	if len(b) == 1 {
		return rune(b[0])
	}
	return rune(uint16(b[0])<<8 | uint16(b[1]))
}
