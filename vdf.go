package main

import (
	"fmt"
	"strings"
)

const maxVDFDepth = 64

// parseVDF parses the Valve KeyValues format used by Steam's .vdf and .acf
// files. Values are either strings or nested map[string]any objects.
func parseVDF(input string) (map[string]any, error) {
	input = strings.TrimPrefix(input, "\ufeff")
	p := &vdfParser{src: []rune(input)}
	obj, err := p.object(false, 0)
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if !p.eof() {
		return nil, fmt.Errorf("unexpected %q at offset %d", p.peek(), p.pos)
	}
	return obj, nil
}

type vdfParser struct {
	src []rune
	pos int
}

func (p *vdfParser) eof() bool {
	return p.pos >= len(p.src)
}

func (p *vdfParser) peek() rune {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}

func (p *vdfParser) skipSpace() {
	for !p.eof() {
		r := p.src[p.pos]
		if r == '/' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '/' {
			p.pos += 2
			for !p.eof() && p.src[p.pos] != '\n' {
				p.pos++
			}
			continue
		}
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			p.pos++
			continue
		}
		return
	}
}

func (p *vdfParser) object(nested bool, depth int) (map[string]any, error) {
	if depth > maxVDFDepth {
		return nil, fmt.Errorf("VDF nesting exceeds %d levels", maxVDFDepth)
	}
	result := make(map[string]any)
	for {
		p.skipSpace()
		if p.eof() {
			if nested {
				return nil, fmt.Errorf("unclosed object at offset %d", p.pos)
			}
			return result, nil
		}
		if p.peek() == '}' {
			if !nested {
				return nil, fmt.Errorf("unexpected } at offset %d", p.pos)
			}
			p.pos++
			return result, nil
		}

		key, err := p.token()
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("empty key at offset %d", p.pos)
		}

		p.skipSpace()
		if p.eof() {
			return nil, fmt.Errorf("missing value for %q", key)
		}
		if p.peek() == '{' {
			p.pos++
			child, err := p.object(true, depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = child
			continue
		}

		value, err := p.token()
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
}

func (p *vdfParser) token() (string, error) {
	if p.eof() {
		return "", fmt.Errorf("unexpected end of file")
	}
	if p.peek() == '"' {
		return p.quoted()
	}

	start := p.pos
	for !p.eof() {
		r := p.src[p.pos]
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '{' || r == '}' {
			break
		}
		p.pos++
	}
	if p.pos == start {
		return "", fmt.Errorf("unexpected %q at offset %d", p.peek(), p.pos)
	}
	return string(p.src[start:p.pos]), nil
}

func (p *vdfParser) quoted() (string, error) {
	p.pos++ // opening quote
	var b strings.Builder
	for !p.eof() {
		r := p.src[p.pos]
		p.pos++
		switch r {
		case '"':
			return b.String(), nil
		case '\\':
			if p.eof() {
				return "", fmt.Errorf("unfinished escape at offset %d", p.pos)
			}
			escaped := p.src[p.pos]
			p.pos++
			switch escaped {
			case 'n':
				b.WriteRune('\n')
			case 'r':
				b.WriteRune('\r')
			case 't':
				b.WriteRune('\t')
			case '\\':
				b.WriteRune('\\')
			case '"':
				b.WriteRune('"')
			default:
				// Valve treats unknown escapes as the escaped character.
				b.WriteRune(escaped)
			}
		default:
			b.WriteRune(r)
		}
	}
	return "", fmt.Errorf("unclosed quoted string")
}

func vdfValue(m map[string]any, key string) (any, bool) {
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

func vdfObject(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := vdfValue(m, key); ok {
		if obj, ok := v.(map[string]any); ok {
			return obj
		}
	}
	return nil
}

func vdfString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := vdfValue(m, key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
