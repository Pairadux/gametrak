// Package steam reads what the Steam client leaves on disk: which games are
// installed (and their names), and its log of the game processes it starts
// and stops. On the Steam Deck that log is how gametrak sees games, since
// there is no Hyprland to report windows.
package steam

import (
	"fmt"
	"strings"
)

// vdfObject is a block of Valve's text KeyValues format, the format of
// libraryfolders.vdf and appmanifest_*.acf. Values are strings or nested
// blocks. Keys are lowercased, since Steam isn't consistent about their case.
type vdfObject map[string]any

// str returns the string value of key, or "".
func (o vdfObject) str(key string) string {
	s, _ := o[key].(string)
	return s
}

// obj returns the nested block under key, or nil.
func (o vdfObject) obj(key string) vdfObject {
	b, _ := o[key].(vdfObject)
	return b
}

// parseVDF reads KeyValues text: quoted (or bare) keys and values, nested
// { } blocks, // comments, and [$PLATFORM] conditions, which are skipped.
// The first of two identical keys wins.
func parseVDF(data string) (vdfObject, error) {
	p := &vdfParser{data: data}
	obj, err := p.object(false)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

type vdfParser struct {
	data string
	pos  int
}

// token kinds
const (
	tokEOF = iota
	tokString
	tokOpen
	tokClose
)

func (p *vdfParser) object(nested bool) (vdfObject, error) {
	obj := vdfObject{}
	for {
		kind, key, err := p.next()
		if err != nil {
			return nil, err
		}
		switch kind {
		case tokEOF:
			if nested {
				return nil, fmt.Errorf("unexpected end of file inside a block")
			}
			return obj, nil
		case tokClose:
			if !nested {
				return nil, fmt.Errorf("unexpected } at byte %d", p.pos)
			}
			return obj, nil
		case tokOpen:
			return nil, fmt.Errorf("unexpected { at byte %d", p.pos)
		}

		kind, value, err := p.next()
		if err != nil {
			return nil, err
		}
		key = strings.ToLower(key)
		switch kind {
		case tokString:
			if _, seen := obj[key]; !seen {
				obj[key] = value
			}
		case tokOpen:
			child, err := p.object(true)
			if err != nil {
				return nil, err
			}
			if _, seen := obj[key]; !seen {
				obj[key] = child
			}
		default:
			return nil, fmt.Errorf("key %q has no value", key)
		}
	}
}

func (p *vdfParser) next() (int, string, error) {
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			p.pos++
		case c == '/' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '/':
			for p.pos < len(p.data) && p.data[p.pos] != '\n' {
				p.pos++
			}
		case c == '[':
			// A platform condition such as [$WIN32]; it applies to the value
			// before it, and the value stands either way.
			for p.pos < len(p.data) && p.data[p.pos] != ']' {
				p.pos++
			}
			p.pos++
		case c == '{':
			p.pos++
			return tokOpen, "", nil
		case c == '}':
			p.pos++
			return tokClose, "", nil
		case c == '"':
			return p.quoted()
		default:
			start := p.pos
			for p.pos < len(p.data) && !strings.ContainsRune(" \t\r\n{}\"", rune(p.data[p.pos])) {
				p.pos++
			}
			return tokString, p.data[start:p.pos], nil
		}
	}
	return tokEOF, "", nil
}

func (p *vdfParser) quoted() (int, string, error) {
	p.pos++ // opening quote
	var b strings.Builder
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		p.pos++
		switch c {
		case '"':
			return tokString, b.String(), nil
		case '\\':
			if p.pos >= len(p.data) {
				return 0, "", fmt.Errorf("unterminated string")
			}
			e := p.data[p.pos]
			p.pos++
			switch e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '\\', '"':
				b.WriteByte(e)
			default:
				b.WriteByte('\\')
				b.WriteByte(e)
			}
		default:
			b.WriteByte(c)
		}
	}
	return 0, "", fmt.Errorf("unterminated string")
}
