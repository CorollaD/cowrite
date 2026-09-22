package voice

import "strings"

// CleanupSystem and CleanupTemplate drive the pass that turns a raw
// transcript into something worth keeping.
//
// The prompt forbids rewriting and expansion in several ways on purpose:
// models treat "clean this up" as an invitation to improve the prose, and
// silently changing what someone said is the one failure that would make
// this feature untrustworthy.
const CleanupSystem = "你是一个口述整理助手。你只做清理，绝不改写、绝不扩写、绝不添加原话没有的内容。"

const CleanupTemplate = `下面是一段语音转写的原文。请把它整理成可读的文字。

必须做的：
- 删掉口水词（嗯、呃、那个、就是说、然后那个……）
- 删掉无意义的重复和结巴
- 识别说话中途的自我更正，只保留改口之后的版本
  例："我们周二……不对，是周三开会" → "我们周三开会"
- 补上标点，按语义分段
- 如果口述内容明显是在列举（第一、第二、还有），整理成 Markdown 列表

绝对不能做的：
- 不要改写原意，不要润色措辞
- 不要增加原话里没有的信息、例子或解释
- 不要删掉有实质内容的句子
- 不要加标题，不要加总结，不要写任何说明性文字

只输出整理后的正文。

原文：
{{.Text}}`

// BuildCleanupPrompt fills the template, optionally adding the user's
// vocabulary so proper nouns the transcriber mangled can be corrected.
func BuildCleanupPrompt(transcript, vocabulary string) string {
	prompt := strings.ReplaceAll(CleanupTemplate, "{{.Text}}", transcript)
	if v := strings.TrimSpace(vocabulary); v != "" {
		prompt += "\n\n这些是可能出现的专有名词，如果转写把它们写错了请改正：" + v
	}
	return prompt
}
