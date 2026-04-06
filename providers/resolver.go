package providers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/smallnest/goclaw/config"
)

// ResolvedProvider contains the resolved provider information
type ResolvedProvider struct {
	ProviderName string                        // Provider name (e.g., "openai", "anthropic")
	ModelID      string                        // Model ID without provider prefix
	BaseURL      string                        // API base URL
	APIKey       string                        // API key (resolved from env or plaintext)
	API          config.ModelAPI               // API type
	Model        *config.ModelDefinitionConfig // Model definition (may be nil)
	Headers      map[string]string             // Custom headers
}

// ProviderResolver resolves provider configuration from model strings
type ProviderResolver struct {
	cfg *config.Config
}

// NewProviderResolver creates a new provider resolver
func NewProviderResolver(cfg *config.Config) *ProviderResolver {
	return &ProviderResolver{cfg: cfg}
}

// lookupProvider resolves provider name with case-insensitive map key match (JSON / env may vary).
func (r *ProviderResolver) lookupProvider(name string) (canonical string, prov *config.ModelProviderConfig) {
	if p := r.cfg.Models.GetProvider(name); p != nil {
		return name, p
	}
	for k := range r.cfg.Models.Providers {
		if strings.EqualFold(k, name) {
			return k, r.cfg.Models.GetProvider(k)
		}
	}
	return "", nil
}

// Resolve resolves a model string to provider configuration
// Model string format: "provider:model-id" or just "model-id"
func (r *ProviderResolver) Resolve(model string) (*ResolvedProvider, error) {
	if !r.cfg.Models.HasProviders() {
		return nil, fmt.Errorf("no providers configured in models.providers. Please configure your providers using OpenClaw-compatible format")
	}

	// Parse provider:model format
	providerName, modelID := parseProviderModel(model)
	if providerName == "" {
		// Try to find the model across all providers
		providerName, _ = r.cfg.Models.FindModelByID(modelID)
		if providerName == "" {
			return nil, fmt.Errorf("model %s not found in any provider", modelID)
		}
	}

	canonical, provider := r.lookupProvider(providerName)
	if provider == nil {
		return nil, fmt.Errorf("provider %s not found", providerName)
	}
	providerName = canonical

	// Find the model definition
	var modelDef *config.ModelDefinitionConfig
	if modelID != "" {
		modelDef = provider.GetModel(modelID)
	}

	// Resolve API key
	var apiKey string
	if strings.EqualFold(providerName, "siliconflow") {
		// SiliconFlow: always allow env + file fallback. Viper AutomaticEnv can wipe apiKey from
		// config to "" and skip ${SILICONFLOW_API_KEY} expansion; direct getenv still works.
		apiKey = resolveSiliconFlowAPIKey(provider.APIKey)
	} else {
		apiKey = strings.TrimSpace(resolveAPIKey(provider.APIKey))
	}
	if apiKey == "" && !providerAllowsEmptyAPIKey(providerName) {
		return nil, unresolvedAPIKeyError(providerName, provider.APIKey)
	}

	// Determine API type
	api := provider.API
	if api == "" {
		api = determineAPIFromProvider(providerName, provider)
	}

	return &ResolvedProvider{
		ProviderName: providerName,
		ModelID:      modelID,
		BaseURL:      provider.BaseURL,
		APIKey:       apiKey,
		API:          api,
		Model:        modelDef,
		Headers:      provider.Headers,
	}, nil
}

// parseProviderModel parses "provider:model-id" format
func parseProviderModel(model string) (provider, modelID string) {
	if idx := strings.Index(model, ":"); idx >= 0 {
		return model[:idx], model[idx+1:]
	}
	if idx := strings.Index(model, "/"); idx >= 0 {
		return model[:idx], model[idx+1:]
	}
	return "", model
}

func providerAllowsEmptyAPIKey(providerName string) bool {
	// Local runtimes often omit auth; caller may still pass a dummy key in config.
	return providerName == "ollama"
}

func unresolvedAPIKeyError(providerName, configured string) error {
	sfHint := ""
	if strings.EqualFold(providerName, "siliconflow") {
		home, _ := os.UserHomeDir()
		if home != "" {
			sfHint = fmt.Sprintf(" Or create file %s with your sk- key on a single line.", filepath.Join(home, ".goclaw", "siliconflow_api_key"))
		}
	}
	if configured == "" {
		return fmt.Errorf("provider %q has no apiKey; set models.providers.%s.apiKey (and export any ${ENV_VAR} you reference).%s", providerName, providerName, sfHint)
	}
	if strings.HasPrefix(configured, "${") && strings.HasSuffix(configured, "}") {
		envName := configured[2 : len(configured)-1]
		return fmt.Errorf("provider %q: environment variable %q is not set or empty (config has %s).%s", providerName, envName, configured, sfHint)
	}
	return fmt.Errorf("provider %q: API key resolved empty (check models.providers.%s.apiKey).%s", providerName, providerName, sfHint)
}

// normalizeSiliconFlowAPIKey removes stray whitespace inside the key (common when pasting from
// PDF/email or Windows env UI accidentally inserts a space mid-token).
func normalizeSiliconFlowAPIKey(k string) string {
	k = strings.TrimSpace(k)
	if k == "" {
		return ""
	}
	k = strings.ReplaceAll(k, " ", "")
	k = strings.ReplaceAll(k, "\n", "")
	k = strings.ReplaceAll(k, "\r", "")
	k = strings.ReplaceAll(k, "\t", "")
	return k
}

// Env vars that may carry the SiliconFlow key (Viper uses GOSKILLS_ + nested path with dots→underscores).
var siliconFlowAPIKeyEnvs = []string{
	"SILICONFLOW_API_KEY",
	"GOSKILLS_MODELS_PROVIDERS_SILICONFLOW_APIKEY",
}

func resolveSiliconFlowAPIKey(configured string) string {
	k := normalizeSiliconFlowAPIKey(resolveAPIKey(configured))
	if k != "" {
		return k
	}
	for _, envName := range siliconFlowAPIKeyEnvs {
		k = normalizeSiliconFlowAPIKey(os.Getenv(envName))
		if k != "" {
			return k
		}
	}
	// Windows: user env from Control Panel is in HKCU\Environment; IDE shells often miss os.Getenv.
	if k = normalizeSiliconFlowAPIKey(readWindowsUserEnv("SILICONFLOW_API_KEY")); k != "" {
		return k
	}
	return normalizeSiliconFlowAPIKey(readSiliconFlowAPIKeyFile())
}

// readSiliconFlowAPIKeyFile loads the API key from ~/.goclaw/siliconflow_api_key (first line, trimmed).
func readSiliconFlowAPIKeyFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(home, ".goclaw", "siliconflow_api_key")
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	return line
}

// resolveAPIKey resolves an API key from environment variable or returns plaintext
func resolveAPIKey(apiKey string) string {
	if apiKey == "" {
		return ""
	}

	// Check if it's an environment variable reference
	if strings.HasPrefix(apiKey, "${") && strings.HasSuffix(apiKey, "}") {
		envVar := apiKey[2 : len(apiKey)-1]
		return os.Getenv(envVar)
	}

	// Check if it's just an environment variable name
	if envValue := os.Getenv(apiKey); envValue != "" {
		return envValue
	}

	// Return as plaintext
	return apiKey
}

// determineAPIFromProvider determines the API type from provider name
func determineAPIFromProvider(providerName string, provider *config.ModelProviderConfig) config.ModelAPI {
	// Check if provider has an API type set
	if provider.API != "" {
		return provider.API
	}

	// Determine from provider name
	switch providerName {
	case "anthropic":
		return config.ModelAPIAnthropicMessages
	case "google", "google-vertex", "google-antigravity":
		return config.ModelAPIGoogleGenAI
	case "ollama":
		return config.ModelAPIOllama
	default:
		return config.ModelAPIOpenAICompletions
	}
}

// GetMaxTokens returns the max tokens for a model
func (r *ResolvedProvider) GetMaxTokens(defaultMax int) int {
	if r.Model != nil && r.Model.MaxTokens > 0 {
		return r.Model.MaxTokens
	}
	return defaultMax
}

// GetContextWindow returns the context window for a model
func (r *ResolvedProvider) GetContextWindow() int {
	if r.Model != nil && r.Model.ContextWindow > 0 {
		return r.Model.ContextWindow
	}
	return 0
}

// IsReasoningModel returns true if the model supports reasoning
func (r *ResolvedProvider) IsReasoningModel() bool {
	return r.Model != nil && r.Model.Reasoning
}

// SupportsImageInput returns true if the model supports image input
func (r *ResolvedProvider) SupportsImageInput() bool {
	return r.Model != nil && r.Model.SupportsInput("image")
}
