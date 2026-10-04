package main

import (
	"errors"
	"unicode"
)

// JSON surgery on ~/.claude.json: only the top-level "oauthAccount" member is ever
// replaced; the rest of the file is preserved byte-for-byte (no decode/re-encode).

func isSpace(b byte) bool { return unicode.IsSpace(rune(b)) }

// jsonValueEnd returns the index just past the value starting at start.
func jsonValueEnd(text string, start int) (int, error) {
	n := len(text)
	if start >= n {
		return 0, errors.New("malformed JSON: missing value")
	}
	switch c := text[start]; {
	case c == '"':
		esc := false
		for i := start + 1; i < n; i++ {
			ch := text[i]
			if esc {
				esc = false
			} else if ch == '\\' {
				esc = true
			} else if ch == '"' {
				return i + 1, nil
			}
		}
		return 0, errors.New("malformed JSON: unterminated string")
	case c == '{' || c == '[':
		depth, inStr, esc := 0, false, false
		for i := start; i < n; i++ {
			ch := text[i]
			if inStr {
				if esc {
					esc = false
				} else if ch == '\\' {
					esc = true
				} else if ch == '"' {
					inStr = false
				}
				continue
			}
			switch ch {
			case '"':
				inStr = true
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1, nil
				}
			}
		}
		return 0, errors.New("malformed JSON: unbalanced brackets")
	default: // number / true / false / null
		for i := start; i < n; i++ {
			ch := text[i]
			if ch == ',' || ch == '}' || ch == ']' || isSpace(ch) {
				return i, nil
			}
		}
		return n, nil
	}
}

// findTopLevelMember locates a depth-1 key. Returns value start/end and whether found.
func findTopLevelMember(text, key string) (valStart, valEnd int, found bool, err error) {
	n := len(text)
	depth, inStr, esc, tokenStart := 0, false, false, -1
	for i := 0; i < n; i++ {
		c := text[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
				if depth == 1 && text[tokenStart+1:i] == key {
					j := i + 1
					for j < n && isSpace(text[j]) {
						j++
					}
					if j < n && text[j] == ':' {
						k := j + 1
						for k < n && isSpace(text[k]) {
							k++
						}
						end, e := jsonValueEnd(text, k)
						if e != nil {
							return 0, 0, false, e
						}
						return k, end, true, nil
					}
				}
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
			tokenStart = i
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
	}
	return 0, 0, false, nil
}

// setTopLevelMember replaces (or inserts) a top-level key's value, leaving everything else intact.
func setTopLevelMember(text, key, valueJSON string) (string, error) {
	s, e, found, err := findTopLevelMember(text, key)
	if err != nil {
		return "", err
	}
	if found {
		return text[:s] + valueJSON + text[e:], nil
	}
	open := -1
	for i := 0; i < len(text); i++ {
		if text[i] == '{' {
			open = i
			break
		}
	}
	if open < 0 {
		return "{\n  \"" + key + "\": " + valueJSON + "\n}\n", nil
	}
	j := open + 1
	for j < len(text) && isSpace(text[j]) {
		j++
	}
	sep := ","
	if j < len(text) && text[j] == '}' {
		sep = ""
	}
	return text[:open+1] + "\n  \"" + key + "\": " + valueJSON + sep + text[open+1:], nil
}
