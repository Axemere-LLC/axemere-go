package gateway

// providerRoute maps a provider name to its upstream host and canonical path.
type providerRoute struct {
	host string
	path string
}

// providerRoutes is the static routing table for all supported providers.
// For "google", the path contains {model} which is replaced at call time.
var providerRoutes = map[string]providerRoute{
	"openai":     {"api.openai.com", "/v1/chat/completions"},
	"anthropic":  {"api.anthropic.com", "/v1/messages"},
	"gemini":     {"generativelanguage.googleapis.com", "/v1beta/models/{model}:generateContent"},
	"cohere":     {"api.cohere.com", "/v2/chat"},
	"mistral":    {"api.mistral.ai", "/v1/chat/completions"},
	"groq":       {"api.groq.com", "/openai/v1/chat/completions"},
	"deepseek":   {"api.deepseek.com", "/v1/chat/completions"},
	"together":   {"api.together.ai", "/v1/chat/completions"},
	"minimax":    {"api.minimax.chat", "/v1/text/chatcompletion_v2"},
	"moonshot":   {"api.moonshot.ai", "/v1/chat/completions"},
	"zhipu":      {"api.z.ai", "/api/paas/v4/chat/completions"},
	"xai":        {"api.x.ai", "/v1/chat/completions"},
	"perplexity": {"api.perplexity.ai", "/chat/completions"},
	"openrouter": {"openrouter.ai", "/api/v1/chat/completions"},
	"nvidia-nim": {"integrate.api.nvidia.com", "/v1/chat/completions"},
	"upstage":    {"api.upstage.ai", "/v1/chat/completions"},
	"fireworks":  {"api.fireworks.ai", "/inference/v1/chat/completions"},
}
