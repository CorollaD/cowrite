package voice

import "strings"

// CommandSystem and CommandTemplate drive spoken editing: the user selects
// text, says what to do with it, and the transcript becomes an instruction
// rather than content to insert.
const CommandSystem = "你是一位中文写作编辑。你严格按用户的口头指令修改选中的文字，不做指令之外的改动。"

const CommandTemplate = `用户选中了一段文字，并口头说出了一条修改指令。请按指令改写这段文字。

指令：{{.Instruction}}

要求：
- 只做指令要求的改动，不要顺手改别的
- 保留原有的 Markdown 标记
- 保持原来的语言
- 只输出改写后的文字，不要解释、不要加引号

选中的文字：
{{.Text}}`

// BuildCommandPrompt combines the spoken instruction with the selection.
func BuildCommandPrompt(instruction, text string) string {
	p := strings.ReplaceAll(CommandTemplate, "{{.Instruction}}", strings.TrimSpace(instruction))
	return strings.ReplaceAll(p, "{{.Text}}", text)
}

// looksLikeInstruction reports whether a transcript reads as a command
// about the text rather than prose to insert.
//
// This only picks a default for the UI; the user can always override which
// mode a recording was meant for.
var instructionHints = []string{
	"改", "换成", "改成", "变成", "重写", "润色", "精简", "缩短", "扩写",
	"翻译", "正式", "口语", "列表", "分段", "删掉", "去掉", "加上",
	"更简洁", "更短", "更长", "语气",
}

func LooksLikeInstruction(transcript string) bool {
	t := strings.TrimSpace(transcript)
	// A long utterance is almost certainly dictation, not a command.
	if len([]rune(t)) > 40 {
		return false
	}
	for _, hint := range instructionHints {
		if strings.Contains(t, hint) {
			return true
		}
	}
	return false
}
