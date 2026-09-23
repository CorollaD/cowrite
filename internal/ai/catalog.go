// Package ai talks to OpenAI-compatible chat and transcription endpoints.
//
// Providers are described as data rather than code: every endpoint cowrite
// speaks to uses the OpenAI wire format, so a provider is just a base URL,
// a model name and how it authenticates. Adding one is a table entry.
package ai

// AuthKind describes what a provider needs before it can be used.
type AuthKind string

const (
	// AuthNone is a provider that works with no credential at all: a local
	// server, or a gateway that is open by default.
	AuthNone AuthKind = "none"
	// AuthAPIKey is a key the user pastes in, held in the OS keyring.
	AuthAPIKey AuthKind = "api_key"
)

// Provider is one configurable endpoint.
type Provider struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	BaseURL string   `json:"baseURL"`
	Auth    AuthKind `json:"auth"`
	// Models are suggestions shown in the picker. Any model string the
	// endpoint accepts still works, since this is not a closed list.
	Models []string `json:"models"`
	// Free marks providers that cost the user nothing to start using,
	// which is what the zero-config default is chosen from.
	Free bool `json:"free"`
	// Notes explains the tradeoff to the user in the settings UI.
	Notes string `json:"notes"`
	// Chat and Voice say what this provider can actually do. DeepSeek has
	// no speech API at all, so offering it for transcription only produces
	// a 404 the user cannot diagnose.
	Chat  bool `json:"chat"`
	Voice bool `json:"voice"`
	// Custom marks a provider that is not OpenAI-compatible and needs its
	// own fields in the settings UI.
	Custom string `json:"custom,omitempty"`
}

// Catalog is the built-in provider list.
//
// The ordering matters: the first usable entry becomes the default, so a
// fresh install works without the user having an account anywhere.
var Catalog = []Provider{
	{
		ID:      "ollama",
		Name:    "Ollama (本地)",
		BaseURL: "http://127.0.0.1:11434/v1",
		Auth:    AuthNone,
		Models:  []string{"qwen3", "llama3.2", "gemma3"},
		Free:    true,
		Notes:   "完全本地运行，不花钱、不联网、稿件不出本机。需要先装 Ollama 并拉一个模型。",

		Chat: true,
	},
	{
		ID:      "lmstudio",
		Name:    "LM Studio (本地)",
		BaseURL: "http://127.0.0.1:1234/v1",
		Auth:    AuthNone,
		Models:  []string{"local-model"},
		Free:    true,
		Notes:   "同样本地运行，带图形界面，适合不想用命令行的用户。",

		Chat: true,
	},
	{
		ID:      "openrouter",
		Name:    "OpenRouter (含免费额度)",
		BaseURL: "https://openrouter.ai/api/v1",
		Auth:    AuthAPIKey,
		Models: []string{
			"deepseek/deepseek-chat-v3.1:free",
			"qwen/qwen3-235b-a22b:free",
			"meta-llama/llama-3.3-70b-instruct:free",
		},
		Free:  true,
		Notes: "注册后拿一个 key，带 :free 后缀的模型不计费（有速率限制）。一个 key 通吃几十家模型。",

		Chat: true,
	},
	{
		ID:      "deepseek",
		Name:    "DeepSeek",
		BaseURL: "https://api.deepseek.com/v1",
		Auth:    AuthAPIKey,
		Models:  []string{"deepseek-chat", "deepseek-reasoner"},
		Notes:   "中文写作质量好且便宜，推荐作为日常主力。",

		Chat: true,
	},
	{
		ID:      "siliconflow",
		Name:    "硅基流动",
		BaseURL: "https://api.siliconflow.cn/v1",
		Auth:    AuthAPIKey,
		Models:  []string{"Qwen/Qwen3-8B", "deepseek-ai/DeepSeek-V3"},
		Free:    true,
		Notes:   "国内直连不用代理，部分小模型免费，也提供语音转写。",

		Chat:  true,
		Voice: true,
	},
	{
		ID:      "groq",
		Name:    "Groq",
		BaseURL: "https://api.groq.com/openai/v1",
		Auth:    AuthAPIKey,
		Models:  []string{"llama-3.3-70b-versatile", "whisper-large-v3-turbo"},
		Free:    true,
		Notes:   "免费额度大、速度极快，语音转写首选。",

		Chat:  true,
		Voice: true,
	},
	{
		ID:      "ark",
		Name:    "火山方舟 (豆包)",
		BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		Auth:    AuthAPIKey,
		Models: []string{
			"doubao-seed-2-0-lite-260428",
			"doubao-pro-32k-241215",
		},
		Notes: "字节跳动豆包大模型，OpenAI 兼容接口。中文写作质量好。注意方舟只有对话，语音识别要用下面的「火山引擎」。",
		Chat:  true,
	},
	{
		ID:      "volcengine",
		Name:    "火山引擎",
		BaseURL: "", // not OpenAI-compatible; handled by its own client
		Auth:    AuthAPIKey,
		Models:  []string{"语音识别"},
		Custom:  "volcengine",
		Notes:   "字节跳动语音识别，中文准确率高。需要在控制台创建应用，填 AppID 和 Access Token。",

		Voice: true,
	},
	{
		ID:      "openai",
		Name:    "OpenAI",
		BaseURL: "https://api.openai.com/v1",
		Auth:    AuthAPIKey,
		Models:  []string{"gpt-4o-mini", "gpt-4o", "whisper-1"},
		Notes:   "质量稳定，按量付费。",

		Chat:  true,
		Voice: true,
	},
}

func FindProvider(id string) (Provider, bool) {
	for _, p := range Catalog {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// FreeProviders returns the providers a user can start with at no cost.
func FreeProviders() []Provider {
	var out []Provider
	for _, p := range Catalog {
		if p.Free {
			out = append(out, p)
		}
	}
	return out
}
