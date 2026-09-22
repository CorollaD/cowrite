package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	tex "github.com/go-tex/math"
	mermaid "github.com/zkrebbekx/go-mermaid"
)

// BlockRenderer turns a fenced block or math expression into SVG.
//
// Both backends are pre-1.0 independent reimplementations, so output will
// not match KaTeX or mermaid.js exactly. The interface keeps them
// replaceable, and a failure falls back to showing the source rather than
// dropping the author's content.
type BlockRenderer interface {
	Render(src string) (svg string, err error)
}

// svgCache memoizes rendered blocks. Preview re-renders on every keystroke,
// and laying out a diagram is far more expensive than parsing markdown.
type svgCache struct {
	mu sync.RWMutex
	m  map[string]string
}

func newSVGCache() *svgCache { return &svgCache{m: make(map[string]string)} }

func (c *svgCache) get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.m[key]
	return v, ok
}

func (c *svgCache) put(key, val string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// A writing session touches few distinct diagrams; clearing wholesale
	// is simpler than tracking use and cannot leak.
	if len(c.m) > 500 {
		c.m = make(map[string]string)
	}
	c.m[key] = val
}

func cacheKey(kind, src string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + src))
	return hex.EncodeToString(sum[:])
}

// mathRenderer renders TeX to SVG using an embedded MATH font, so there is
// no external dependency and nothing to install.
type mathRenderer struct {
	once sync.Once
	r    *tex.Renderer
	err  error
}

func (m *mathRenderer) renderer() (*tex.Renderer, error) {
	m.once.Do(func() {
		m.r, m.err = tex.New(tex.DefaultFont())
	})
	return m.r, m.err
}

func (m *mathRenderer) render(src string, display bool) (string, error) {
	r, err := m.renderer()
	if err != nil {
		return "", fmt.Errorf("math renderer unavailable: %w", err)
	}
	if display {
		return r.RenderDisplaySVG(src, 20)
	}
	return r.RenderSVG(src, 16)
}

// renderMermaid lays out a diagram entirely in Go: no Node, no headless
// browser, so the single-binary promise holds.
func renderMermaid(src string) (string, error) {
	out, err := mermaid.Render(src)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
