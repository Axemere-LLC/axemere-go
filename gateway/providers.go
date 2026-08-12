package gateway

// providerRoute maps a provider name to its upstream host and canonical path.
type providerRoute struct {
	host string
	path string
}

// providerRoutes is the static routing table for all supported providers.
// For "gemini"/"google", the path contains {model} which is replaced at
// call time. "bedrock" and "vertex" derive their upstream region from the
// credential server-side and never use path; their host exists only to
// satisfy the gateway's multi-host auto-policy rule. "azure_openai" has no
// static host — callers must supply ExecuteRequest.TargetHost.
var providerRoutes = map[string]providerRoute{
	"openai":       {"api.openai.com", "/v1/chat/completions"},
	"anthropic":    {"api.anthropic.com", "/v1/messages"},
	"gemini":       {"generativelanguage.googleapis.com", "/v1beta/models/{model}:generateContent"},
	"google":       {"generativelanguage.googleapis.com", "/v1beta/models/{model}:generateContent"},
	"cohere":       {"api.cohere.com", "/v2/chat"},
	"mistral":      {"api.mistral.ai", "/v1/chat/completions"},
	"groq":         {"api.groq.com", "/openai/v1/chat/completions"},
	"deepseek":     {"api.deepseek.com", "/v1/chat/completions"},
	"together":     {"api.together.ai", "/v1/chat/completions"},
	"minimax":      {"api.minimax.io", "/v1/chat/completions"},
	"moonshot":     {"api.moonshot.ai", "/v1/chat/completions"},
	"zhipu":        {"api.z.ai", "/api/paas/v4/chat/completions"},
	"xai":          {"api.x.ai", "/v1/chat/completions"},
	"perplexity":   {"api.perplexity.ai", "/chat/completions"},
	"openrouter":   {"openrouter.ai", "/api/v1/chat/completions"},
	"nvidia-nim":   {"integrate.api.nvidia.com", "/v1/chat/completions"},
	"upstage":      {"api.upstage.ai", "/v1/chat/completions"},
	"fireworks":    {"api.fireworks.ai", "/inference/v1/chat/completions"},
	"qwen":         {"dashscope-intl.aliyuncs.com", "/v1/chat/completions"},
	"bytedance":    {"ark.cn-beijing.volces.com", "/api/v3/chat/completions"},
	"stepfun":      {"api.stepfun.com", "/v1/chat/completions"},
	"bedrock":      {"bedrock-runtime.us-east-1.amazonaws.com", ""},
	"vertex":       {"us-central1-aiplatform.googleapis.com", ""},
	"azure_openai": {"", "/v1/chat/completions"},
}
