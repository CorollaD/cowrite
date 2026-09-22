package render

import (
	"fmt"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
)

// ToMarkdown converts edited rich text back into markdown source.
//
// The file on disk stays markdown regardless of which mode was used to
// write it, so switching modes never changes what is stored.
func ToMarkdown(htmlStr string) (string, error) {
	md, err := htmltomarkdown.ConvertString(htmlStr)
	if err != nil {
		return "", fmt.Errorf("convert html to markdown: %w", err)
	}
	// contenteditable leaves trailing blank lines as people edit.
	return strings.TrimRight(md, " \n\t") + "\n", nil
}
