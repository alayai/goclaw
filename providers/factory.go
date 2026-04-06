package providers

import (
	"fmt"
	"strings"
	"time"

	"github.com/smallnest/goclaw/config"
)

// NewProvider 创建提供商
func NewProvider(cfg *config.Config) (Provider, error) {
	return NewSimpleProvider(cfg)
}

// NewSimpleProvider 创建单一提供商
func NewSimpleProvider(cfg *config.Config) (Provider, error) {
	// 只支持 OpenClaw 风格的配置
	if !cfg.Models.HasProviders() {
		return nil, fmt.Errorf("no LLM provider configured. Please configure models.providers in your config file")
	}
	return NewProviderFromModelsConfig(cfg)
}

// NewProviderFromModelsConfig 从 OpenClaw 风格的 models.providers 配置创建提供商
func NewProviderFromModelsConfig(cfg *config.Config) (Provider, error) {
	resolver := NewProviderResolver(cfg)
	resolved, err := resolver.Resolve(cfg.Agents.Defaults.Model.Effective())
	if err != nil {
		return nil, fmt.Errorf("failed to resolve provider: %w", err)
	}

	// 默认超时 120 秒
	timeout := 120 * time.Second
	maxTokens := resolved.GetMaxTokens(cfg.Agents.Defaults.MaxTokens)

	apiKey := strings.TrimSpace(resolved.APIKey)
	if apiKey == "" && strings.EqualFold(resolved.ProviderName, "ollama") {
		// Local Ollama ignores the token; langchaingo still expects a non-empty string.
		apiKey = "ollama"
	}
	// Defense in depth: stale binaries or edge unmarshaling can still yield an empty resolved key.
	if apiKey == "" && strings.EqualFold(resolved.ProviderName, "siliconflow") {
		var raw string
		if p := cfg.Models.GetProvider(resolved.ProviderName); p != nil {
			raw = p.APIKey
		} else {
			for k := range cfg.Models.Providers {
				if strings.EqualFold(k, "siliconflow") {
					if p2 := cfg.Models.GetProvider(k); p2 != nil {
						raw = p2.APIKey
						break
					}
				}
			}
		}
		apiKey = resolveSiliconFlowAPIKey(raw)
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("missing LLM API key for provider %q: set env SILICONFLOW_API_KEY and reopen the terminal (Windows user vars are not picked up by old shells), or GOSKILLS_MODELS_PROVIDERS_SILICONFLOW_APIKEY, or put the key in %%USERPROFILE%%\\.goclaw\\siliconflow_api_key", resolved.ProviderName)
	}

	// 根据 API 类型创建提供商
	switch resolved.API {
	case config.ModelAPIAnthropicMessages:
		return NewAnthropicProviderWithTimeout(apiKey, resolved.BaseURL, resolved.ModelID, maxTokens, timeout)
	case config.ModelAPIOpenAICompletions:
		return NewOpenAIProviderWithTimeout(apiKey, resolved.BaseURL, resolved.ModelID, maxTokens, timeout)
	case config.ModelAPIOllama:
		return NewOpenAIProviderWithTimeout(apiKey, resolved.BaseURL, resolved.ModelID, maxTokens, timeout)
	default:
		// 默认使用 OpenAI 兼容的 API
		return NewOpenAIProviderWithTimeout(apiKey, resolved.BaseURL, resolved.ModelID, maxTokens, timeout)
	}
}
