package ai

import (
	"fmt"
	"strings"
)

// Scope says what text a command operates on.
type Scope string

const (
	ScopeSelection Scope = "selection" // the highlighted text
	ScopeDocument  Scope = "document"  // the whole post
	ScopeCursor    Scope = "cursor"    // everything before the cursor
)

// Command is one AI action offered in the editor.
type Command struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Scope       Scope   `json:"scope"`
	Temperature float64 `json:"-"`
	System      string  `json:"-"`
	Template    string  `json:"-"`
}

// maxContext bounds how much of a document is sent upstream. Long posts are
// trimmed from the middle so the beginning and end, which carry the most
// signal about voice and topic, both survive.
const maxContext = 6000

// Commands are the actions the editor exposes.
//
// Each prompt is explicit about preserving markdown and the author's
// meaning: models otherwise flatten structure and quietly rewrite intent.
var Commands = []Command{
	{
		ID:          "polish",
		Label:       "润色",
		Scope:       ScopeSelection,
		Temperature: 0.3,
		System:      "你是一位中文写作编辑。你只改表达，不改意思。",
		Template: `请润色下面这段文字，让它更清楚、更自然。

要求：
- 保持原意不变，不要增加原文没有的信息
- 保留所有 Markdown 标记（标题、列表、加粗、代码等）
- 保持原有的语言（中文就用中文，英文就用英文）
- 只输出润色后的文字，不要解释、不要加引号

原文：
{{.Text}}`,
	},
	{
		ID:          "continue",
		Label:       "续写",
		Scope:       ScopeCursor,
		Temperature: 0.7,
		System:      "你是一位中文写作助手，擅长模仿作者已有的语气继续往下写。",
		Template: `下面是一篇文章目前写到的部分。请接着往下写。

要求：
- 模仿已有的语气、人称和节奏
- 接着最后一句自然往下写，不要重复已有内容
- 用 Markdown 格式
- 只输出续写的部分

已有内容：
{{.Text}}`,
	},
	{
		ID:          "title",
		Label:       "起标题",
		Scope:       ScopeDocument,
		Temperature: 0.8,
		System:      "你是一位擅长起标题的编辑。",
		Template: `根据下面这篇文章，给出 5 个候选标题。

要求：
- 每行一个，前面不要编号、不要符号
- 具体、有信息量，不要用"浅谈""漫谈"这类套话
- 控制在 30 字以内

文章：
{{.Text}}`,
	},
	{
		ID:          "summary",
		Label:       "摘要",
		Scope:       ScopeDocument,
		Temperature: 0.3,
		System:      "你是一位中文编辑。",
		Template: `给下面这篇文章写一段摘要。

要求：
- 不超过 120 字（公众号摘要字段的上限）
- 说清楚文章讲了什么，不要用"本文介绍了"这类开头
- 只输出摘要本身

文章：
{{.Text}}`,
	},
}

func FindCommand(id string) (Command, bool) {
	for _, c := range Commands {
		if c.ID == id {
			return c, true
		}
	}
	return Command{}, false
}

// Render fills a command's template with the text it operates on.
func (c Command) Render(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("没有选中或可用的文字")
	}
	return strings.ReplaceAll(c.Template, "{{.Text}}", Truncate(text)), nil
}

// Truncate keeps a long document within the context budget, cutting from
// the middle and saying so rather than silently losing the ending.
func Truncate(s string) string {
	r := []rune(s)
	if len(r) <= maxContext {
		return s
	}
	head := maxContext / 2
	tail := maxContext - head
	return string(r[:head]) +
		"\n\n……（此处省略中间部分）……\n\n" +
		string(r[len(r)-tail:])
}
