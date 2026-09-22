package render

import (
	"embed"
	"sort"
	"sync"
)

//go:embed themes/*.css
var themeFS embed.FS

// Theme is a named stylesheet written against the .cw-root class contract.
type Theme struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	CSS  string `json:"-"`
}

var (
	themesOnce sync.Once
	themes     map[string]Theme
	themeOrder []string
)

// themeNames maps a stylesheet file to its display name. A theme without an
// entry here still loads, titled by its ID.
var themeNames = map[string]string{
	"default": "默认",
	"serif":   "衬线",
}

func loadThemes() {
	themesOnce.Do(func() {
		themes = make(map[string]Theme)
		entries, err := themeFS.ReadDir("themes")
		if err != nil {
			return
		}
		for _, e := range entries {
			id := e.Name()[:len(e.Name())-len(".css")]
			css, err := themeFS.ReadFile("themes/" + e.Name())
			if err != nil {
				continue
			}
			name := themeNames[id]
			if name == "" {
				name = id
			}
			themes[id] = Theme{ID: id, Name: name, CSS: string(css)}
			themeOrder = append(themeOrder, id)
		}
		sort.Slice(themeOrder, func(i, j int) bool {
			// "default" first, then alphabetical.
			if themeOrder[i] == "default" {
				return true
			}
			if themeOrder[j] == "default" {
				return false
			}
			return themeOrder[i] < themeOrder[j]
		})
	})
}

func FindTheme(id string) (Theme, bool) {
	loadThemes()
	t, ok := themes[id]
	return t, ok
}

func DefaultTheme() Theme {
	loadThemes()
	if t, ok := themes["default"]; ok {
		return t
	}
	// Should not happen: the default stylesheet is embedded.
	return Theme{ID: "default", Name: "默认", CSS: ""}
}

// Themes lists the available themes in display order.
func Themes() []Theme {
	loadThemes()
	out := make([]Theme, 0, len(themeOrder))
	for _, id := range themeOrder {
		out = append(out, themes[id])
	}
	return out
}
