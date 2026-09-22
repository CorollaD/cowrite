package workspace

import (
	"bytes"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

var frontMatterFence = []byte("---")

// Meta is the YAML front matter carried at the top of every post file.
//
// ID is a UUID rather than the file path: paths change when a post is
// renamed or refiled, and the index has to survive that.
type Meta struct {
	ID      string    `yaml:"id"`
	Title   string    `yaml:"title"`
	Created time.Time `yaml:"created"`
	Updated time.Time `yaml:"updated"`
	Tags    []string  `yaml:"tags,omitempty"`
	Cover   string    `yaml:"cover,omitempty"`
}

// Post is a parsed post file: its front matter plus the markdown body.
type Post struct {
	Meta Meta
	Body string
}

// ParsePost splits a post file into front matter and body. A file with no
// front matter is treated as a body-only post so that markdown dropped into
// the workspace by hand still opens; the caller assigns it an ID on save.
func ParsePost(data []byte) (*Post, error) {
	rest, ok := bytes.CutPrefix(data, frontMatterFence)
	if !ok {
		return &Post{Body: string(data)}, nil
	}
	// The opening fence must be its own line.
	rest = bytes.TrimLeft(rest, "\r")
	if !bytes.HasPrefix(rest, []byte("\n")) {
		return &Post{Body: string(data)}, nil
	}
	rest = rest[1:]

	end := bytes.Index(rest, append([]byte("\n"), frontMatterFence...))
	if end < 0 {
		// Unterminated front matter: treat the whole file as body rather
		// than losing content to a parse error.
		return &Post{Body: string(data)}, nil
	}

	raw, body := rest[:end], rest[end+1+len(frontMatterFence):]
	// Drop the line terminator that ends the closing fence line, plus one
	// blank separator line if present, so the body starts at real content.
	body = bytes.TrimPrefix(body, []byte("\r"))
	body = bytes.TrimPrefix(body, []byte("\n"))
	body = bytes.TrimPrefix(body, []byte("\r"))
	body = bytes.TrimPrefix(body, []byte("\n"))

	var meta Meta
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("parse front matter: %w", err)
	}
	return &Post{Meta: meta, Body: string(body)}, nil
}

// Bytes renders a post back to its on-disk form.
func (p *Post) Bytes() ([]byte, error) {
	raw, err := yaml.Marshal(p.Meta)
	if err != nil {
		return nil, fmt.Errorf("encode front matter: %w", err)
	}
	var buf bytes.Buffer
	buf.Write(frontMatterFence)
	buf.WriteByte('\n')
	buf.Write(raw)
	buf.Write(frontMatterFence)
	buf.WriteString("\n\n")
	buf.WriteString(p.Body)
	return buf.Bytes(), nil
}

// EnsureIDs fills in the fields a new or hand-authored post is missing.
func (p *Post) EnsureIDs(now time.Time) {
	if p.Meta.ID == "" {
		p.Meta.ID = uuid.NewString()
	}
	if p.Meta.Created.IsZero() {
		p.Meta.Created = now
	}
	if p.Meta.Title == "" {
		p.Meta.Title = DeriveTitle(p.Body)
	}
	p.Meta.Updated = now
}

// DeriveTitle picks a title from the body: the first ATX heading if there is
// one, otherwise the first non-empty line.
func DeriveTitle(body string) string {
	for line := range strings.Lines(body) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if h, ok := strings.CutPrefix(line, "#"); ok {
			if t := strings.TrimSpace(strings.TrimLeft(h, "#")); t != "" {
				return t
			}
			continue
		}
		return line
	}
	return "Untitled"
}

// WordCount counts CJK characters individually and runs of Latin script as
// words, which is what a mixed Chinese/English draft needs.
func WordCount(body string) int {
	count := 0
	inWord := false
	for _, r := range body {
		switch {
		case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
			count++
			inWord = false
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			if !inWord {
				count++
				inWord = true
			}
		default:
			inWord = false
		}
	}
	return count
}
