package llmprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type InvalidateFunc func(ctx context.Context)

type Resolved struct {
	Entries           []ProviderReloadEntry
	Default           domain.LLMProviderType
	EmbeddingProvider domain.LLMProviderType
	EmbeddingModel    string
}

type ProviderReloadEntry struct {
	ProviderType   domain.LLMProviderType
	BaseURL        string
	DefaultModel   string
	APIKey         string
	TimeoutSeconds int
}

type Service struct {
	store     port.LLMProviderStore
	endpoints port.LLMEndpointStore
	cipher    *secrets.Cipher
	timeout   time.Duration

	invalidate InvalidateFunc

	afterChange func(ctx context.Context)

	bundledEmbedderURL    string
	bundledEmbedderSource string
}

func (s *Service) SetAfterChange(fn func(ctx context.Context)) {
	s.afterChange = fn
}

func (s *Service) notifyChange(ctx context.Context) {
	if s.afterChange != nil {
		s.afterChange(ctx)
	}
}

func NewService(store port.LLMProviderStore, endpoints port.LLMEndpointStore, cipher *secrets.Cipher, timeout time.Duration, invalidate InvalidateFunc) *Service {
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &Service{store: store, endpoints: endpoints, cipher: cipher, timeout: timeout, invalidate: invalidate}
}

func (s *Service) ResolvedEmbedding(ctx context.Context) (model string, dimensions int, err error) {
	model, err = s.store.GetEmbeddingModel(ctx)
	if err != nil {
		return "", 0, err
	}
	if model == domain.PinnedLocalEmbeddingModel {
		dimensions = domain.PinnedLocalEmbeddingDimensions
	}
	return model, dimensions, nil
}

func (s *Service) EmbeddingStatus(ctx context.Context) (domain.EmbeddingStatus, error) {
	provider, err := s.store.GetEmbeddingProvider(ctx)
	if err != nil {
		return domain.EmbeddingStatus{}, err
	}
	model, err := s.store.GetEmbeddingModel(ctx)
	if err != nil {
		return domain.EmbeddingStatus{}, err
	}
	out := domain.EmbeddingStatus{Provider: provider, Model: model}
	if model == domain.PinnedLocalEmbeddingModel {
		out.Dimensions = domain.PinnedLocalEmbeddingDimensions
	}
	return out, nil
}

func (s *Service) List(ctx context.Context) (domain.LLMProvidersResponse, error) {
	stored, err := s.store.List(ctx)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	active, err := s.store.GetActiveProvider(ctx)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	byType := make(map[domain.LLMProviderType]domain.LLMProviderConfig, len(stored))
	for _, cfg := range stored {
		byType[cfg.ProviderType] = cfg
	}

	providers := make([]domain.LLMProviderView, 0, len(domain.AllLLMProviderDefinitions()))
	for _, def := range domain.AllLLMProviderDefinitions() {
		cfg, ok := byType[def.Type]
		if !ok {
			cfg = domain.LLMProviderConfig{
				ProviderType: def.Type,
				BaseURL:      def.DefaultBaseURL,
				DefaultModel: def.DefaultModel,
			}
		}
		providers = append(providers, domain.LLMProviderView{
			Definition: def,
			Config:     cfg,
			Active:     active == def.Type,
		})
	}
	embedding, err := s.store.GetEmbeddingProvider(ctx)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	embeddingModel, err := s.store.GetEmbeddingModel(ctx)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	endpoints := []domain.LLMEndpoint{}
	if s.endpoints != nil {
		eps, err := s.endpoints.List(ctx)
		if err != nil {
			return domain.LLMProvidersResponse{}, err
		}
		if eps != nil {
			endpoints = eps
		}
	}
	return domain.LLMProvidersResponse{
		ActiveProvider:    active,
		EmbeddingProvider: embedding,
		EmbeddingModel:    embeddingModel,
		Providers:         providers,
		Endpoints:         endpoints,
	}, nil
}

func errHostExecuted(providerType domain.LLMProviderType) error {
	return fmt.Errorf("%s runs as a process on a machine rather than as an endpoint on the network — in cloud the assigned "+
		"member's own machine, on a self-hosted install the host this server runs on — so there is nothing to connect, test or "+
		"activate. Select it as an agent's provider instead: %w", providerType, domain.ErrHostExecutedUnservable)
}

func (s *Service) Connect(ctx context.Context, providerType domain.LLMProviderType, req domain.ConnectLLMProviderRequest) (domain.LLMProvidersResponse, error) {
	def, ok := domain.LLMProviderDefinitionFor(providerType)
	if !ok {
		return domain.LLMProvidersResponse{}, fmt.Errorf("invalid provider type: %s", providerType)
	}

	if !def.Available {
		return domain.LLMProvidersResponse{}, domain.ErrUnavailableProvider(providerType)
	}
	if def.HostExecuted {
		return domain.LLMProvidersResponse{}, errHostExecuted(providerType)
	}

	apiKey := strings.TrimSpace(req.APIKey)
	existing, _ := s.store.Get(ctx, providerType)

	baseURL := firstNonBlank(req.BaseURL, def.DefaultBaseURL)
	if def.BaseURLRequired && baseURL == "" {
		return domain.LLMProvidersResponse{}, fmt.Errorf("base_url is required")
	}

	defaultModel := firstNonBlank(req.DefaultModel, existing.DefaultModel, def.DefaultModel)
	if def.ModelRequired && defaultModel == "" {
		return domain.LLMProvidersResponse{}, fmt.Errorf("default_model is required")
	}

	if def.RequiresAPIKey && apiKey == "" && !existing.HasAPIKey {
		return domain.LLMProvidersResponse{}, fmt.Errorf("api_key is required")
	}

	timeoutSeconds := resolveTimeoutSeconds(req.TimeoutSeconds, existing.TimeoutSeconds, providerType)
	if timeoutSeconds < 30 {
		return domain.LLMProvidersResponse{}, fmt.Errorf("timeout_seconds must be at least 30")
	}
	if timeoutSeconds > 3600 {
		return domain.LLMProvidersResponse{}, fmt.Errorf("timeout_seconds must be at most 3600")
	}

	if err := s.testClient(providerType, baseURL, defaultModel, s.resolveAPIKey(apiKey, existing.HasAPIKey, ctx, providerType), timeoutSeconds); err != nil {
		return domain.LLMProvidersResponse{}, fmt.Errorf("connection test failed: %w", err)
	}

	cfg := domain.LLMProviderConfig{
		ProviderType:   providerType,
		BaseURL:        baseURL,
		DefaultModel:   defaultModel,
		TimeoutSeconds: timeoutSeconds,
		Configured:     true,
	}
	if err := s.store.Upsert(ctx, cfg); err != nil {
		return domain.LLMProvidersResponse{}, err
	}

	if apiKey != "" {
		if s.cipher == nil {
			return domain.LLMProvidersResponse{}, fmt.Errorf("secret storage unavailable")
		}
		encrypted, err := s.cipher.Encrypt(apiKey)
		if err != nil {
			return domain.LLMProvidersResponse{}, err
		}
		if err := s.store.SetAPIKey(ctx, providerType, encrypted); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	}

	currentActive, _ := s.store.GetActiveProvider(ctx)
	if currentActive == "" || currentActive == providerType {
		if err := s.store.SetActiveProvider(ctx, providerType); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	} else {
		currentCfg, _ := s.store.Get(ctx, currentActive)
		if !currentCfg.Configured {
			if err := s.store.SetActiveProvider(ctx, providerType); err != nil {
				return domain.LLMProvidersResponse{}, err
			}
		}
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	s.notifyChange(ctx)
	return s.List(ctx)
}

func (s *Service) BootstrapEmbeddings(ctx context.Context, baseURL string) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	const providerType = domain.LLMProviderLocal

	existing, _ := s.store.Get(ctx, providerType)

	if existing.Configured && existing.DefaultModel != "" && existing.DefaultModel != domain.PinnedLocalEmbeddingModel {
		log.Info().Str("model", existing.DefaultModel).
			Msg("embeddings: the OpenAI-compatible provider is configured for another model; leaving it and the embedding choice alone")
		return nil
	}
	if !existing.Configured || existing.BaseURL != baseURL || existing.DefaultModel == "" {
		if err := s.store.Upsert(ctx, domain.LLMProviderConfig{
			ProviderType:   providerType,
			BaseURL:        baseURL,
			DefaultModel:   firstNonBlank(existing.DefaultModel, domain.PinnedLocalEmbeddingModel),
			TimeoutSeconds: resolveTimeoutSeconds(existing.TimeoutSeconds, 0, providerType),
			Configured:     true,
		}); err != nil {
			return err
		}
	}

	chosen, _ := s.store.GetEmbeddingProvider(ctx)
	if chosen != "" && chosen != providerType {
		return s.reloadAllConfigured(ctx)
	}
	if err := s.store.SetEmbeddingProvider(ctx, providerType); err != nil {
		return err
	}
	if model, _ := s.store.GetEmbeddingModel(ctx); model == "" {
		if err := s.store.SetEmbeddingModel(ctx, domain.PinnedLocalEmbeddingModel); err != nil {
			return err
		}
	}
	return s.reloadAllConfigured(ctx)
}

func (s *Service) BootstrapFromEnv(ctx context.Context) {
	type candidate struct {
		providerType domain.LLMProviderType
		envKey       string
	}
	for _, c := range []candidate{
		{domain.LLMProviderAnthropic, "ANTHROPIC_API_KEY"},
		{domain.LLMProviderOpenAI, "OPENAI_API_KEY"},
	} {
		apiKey := os.Getenv(c.envKey)
		if apiKey == "" {
			continue
		}
		existing, _ := s.store.Get(ctx, c.providerType)
		if existing.Configured {
			continue
		}
		def, _ := domain.LLMProviderDefinitionFor(c.providerType)
		_, _ = s.Connect(ctx, c.providerType, domain.ConnectLLMProviderRequest{
			BaseURL:        def.DefaultBaseURL,
			APIKey:         apiKey,
			TimeoutSeconds: def.DefaultTimeoutSeconds,
		})
	}

	if apiKey := os.Getenv("GOOGLE_API_KEY"); apiKey != "" {
		existing, _ := s.store.Get(ctx, domain.LLMProviderGemini)
		if !existing.Configured {
			def, _ := domain.LLMProviderDefinitionFor(domain.LLMProviderGemini)
			_, _ = s.Connect(ctx, domain.LLMProviderGemini, domain.ConnectLLMProviderRequest{
				BaseURL:        def.DefaultBaseURL,
				APIKey:         apiKey,
				TimeoutSeconds: def.DefaultTimeoutSeconds,
			})
		}
	}
}

func (s *Service) Activate(ctx context.Context, providerType domain.LLMProviderType) (domain.LLMProvidersResponse, error) {

	if domain.ValidLLMProviderType(string(providerType)) && !domain.ProviderAvailable(providerType) {
		return domain.LLMProvidersResponse{}, domain.ErrUnavailableProvider(providerType)
	}
	if domain.RequiresHostExecutor(providerType) {
		return domain.LLMProvidersResponse{}, errHostExecuted(providerType)
	}
	if domain.ValidLLMProviderType(string(providerType)) {
		cfg, err := s.store.Get(ctx, providerType)
		if err != nil {
			return domain.LLMProvidersResponse{}, err
		}
		if !cfg.Configured {
			return domain.LLMProvidersResponse{}, fmt.Errorf("provider is not configured")
		}
	} else if s.endpoints != nil {
		ep, err := s.endpoints.Get(ctx, string(providerType))
		if err != nil {
			return domain.LLMProvidersResponse{}, fmt.Errorf("invalid provider ref: %s", providerType)
		}
		if !ep.Configured {
			return domain.LLMProvidersResponse{}, fmt.Errorf("endpoint is not configured")
		}
	} else {
		return domain.LLMProvidersResponse{}, fmt.Errorf("invalid provider type: %s", providerType)
	}
	if err := s.store.SetActiveProvider(ctx, providerType); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	s.notifyChange(ctx)
	return s.List(ctx)
}

func (s *Service) Disconnect(ctx context.Context, providerType domain.LLMProviderType) (domain.LLMProvidersResponse, error) {
	def, ok := domain.LLMProviderDefinitionFor(providerType)
	if !ok {
		return domain.LLMProvidersResponse{}, fmt.Errorf("invalid provider type: %s", providerType)
	}
	cfg := domain.LLMProviderConfig{
		ProviderType:   providerType,
		BaseURL:        def.DefaultBaseURL,
		DefaultModel:   def.DefaultModel,
		TimeoutSeconds: 0,
		Configured:     false,
	}
	if err := s.store.Upsert(ctx, cfg); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if err := s.store.DeleteAPIKey(ctx, providerType); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	active, err := s.store.GetActiveProvider(ctx)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if active == providerType {
		if err := s.store.SetActiveProvider(ctx, domain.LLMProviderLocal); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	s.notifyChange(ctx)
	return s.List(ctx)
}

func (s *Service) Test(ctx context.Context, providerType domain.LLMProviderType, req domain.TestLLMProviderRequest) error {
	def, ok := domain.LLMProviderDefinitionFor(providerType)
	if !ok {
		return fmt.Errorf("invalid provider type: %s", providerType)
	}
	if !def.Available {
		return domain.ErrUnavailableProvider(providerType)
	}
	if def.HostExecuted {
		return errHostExecuted(providerType)
	}
	existing, _ := s.store.Get(ctx, providerType)

	baseURL := firstNonBlank(req.BaseURL, existing.BaseURL, def.DefaultBaseURL)
	defaultModel := firstNonBlank(req.DefaultModel, existing.DefaultModel, def.DefaultModel)
	apiKey := s.resolveAPIKey(strings.TrimSpace(req.APIKey), existing.HasAPIKey, ctx, providerType)
	timeoutSeconds := resolveTimeoutSeconds(req.TimeoutSeconds, existing.TimeoutSeconds, providerType)
	return s.testClient(providerType, baseURL, defaultModel, apiKey, timeoutSeconds)
}

func (s *Service) reloadAllConfigured(ctx context.Context) error {
	if s.invalidate != nil {
		s.invalidate(ctx)
	}
	return nil
}

func (s *Service) Resolve(ctx context.Context) (Resolved, error) {
	stored, err := s.store.List(ctx)
	if err != nil {
		return Resolved{}, err
	}
	active, err := s.store.GetActiveProvider(ctx)
	if err != nil {
		return Resolved{}, err
	}
	entries := make([]ProviderReloadEntry, 0, len(stored))
	for _, cfg := range stored {
		if !cfg.Configured {
			continue
		}
		if cfg.TimeoutSeconds == 0 {
			cfg.TimeoutSeconds = resolveTimeoutSeconds(0, 0, cfg.ProviderType)
			if err := s.store.Upsert(ctx, cfg); err != nil {
				return Resolved{}, err
			}
		}
		apiKey, err := s.decryptAPIKey(ctx, cfg.ProviderType)
		if err != nil {
			return Resolved{}, err
		}
		entries = append(entries, ProviderReloadEntry{
			ProviderType:   cfg.ProviderType,
			BaseURL:        cfg.BaseURL,
			DefaultModel:   cfg.DefaultModel,
			APIKey:         apiKey,
			TimeoutSeconds: cfg.TimeoutSeconds,
		})
	}

	if s.endpoints != nil {
		eps, err := s.endpoints.List(ctx)
		if err != nil {
			return Resolved{}, err
		}
		for _, ep := range eps {
			if !ep.Configured {
				continue
			}
			apiKey, err := s.decryptEndpointAPIKey(ctx, ep.ID)
			if err != nil {
				return Resolved{}, err
			}
			entries = append(entries, ProviderReloadEntry{
				ProviderType:   domain.LLMProviderType(ep.ID),
				BaseURL:        ep.BaseURL,
				DefaultModel:   ep.DefaultModel,
				APIKey:         apiKey,
				TimeoutSeconds: ep.TimeoutSeconds,
			})
		}
	}
	embedding, err := s.store.GetEmbeddingProvider(ctx)
	if err != nil {
		return Resolved{}, err
	}
	embeddingModel, err := s.store.GetEmbeddingModel(ctx)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{
		Entries:           entries,
		Default:           active,
		EmbeddingProvider: embedding,
		EmbeddingModel:    embeddingModel,
	}, nil
}

func (s *Service) EmbeddingProvider(ctx context.Context) (domain.LLMProviderType, error) {
	return s.store.GetEmbeddingProvider(ctx)
}

func errNoEmbeddingsFromProvider() error {
	return fmt.Errorf("this provider cannot produce embeddings")
}

func errNoSuchEndpoint(id string) error {
	return fmt.Errorf("no such endpoint: %s", id)
}

func endpointRef(ref domain.LLMProviderType) bool {
	_, err := uuid.Parse(string(ref))
	return err == nil
}

func (s *Service) ListEmbeddingModels(ctx context.Context, providerType domain.LLMProviderType) ([]string, error) {

	if !domain.ValidLLMProviderType(string(providerType)) {
		if s.endpoints == nil || !endpointRef(providerType) {
			return nil, errNoEmbeddingsFromProvider()
		}
		ep, err := s.endpoints.Get(ctx, string(providerType))
		if err != nil {

			return nil, errNoEmbeddingsFromProvider()
		}
		if !ep.Configured || ep.BaseURL == "" {
			return nil, fmt.Errorf("provider is not connected")
		}
		if models, err := lmStudioEmbeddingModels(ctx, ep.BaseURL); err == nil && len(models) > 0 {
			return models, nil
		}
		apiKey, err := s.decryptEndpointAPIKey(ctx, ep.ID)
		if err != nil {
			return nil, fmt.Errorf("this endpoint's stored API key could not be read; reconnect it with the key again")
		}
		models, err := openAICompatibleModels(ctx, ep.BaseURL, apiKey)
		if err != nil {
			return nil, err
		}
		return preferEmbeddingModels(models), nil
	}
	cfg, err := s.store.Get(ctx, providerType)
	if err != nil {
		return nil, errNoEmbeddingsFromProvider()
	}
	if !cfg.Configured || cfg.BaseURL == "" {
		return nil, fmt.Errorf("provider is not connected")
	}
	switch providerType {
	case domain.LLMProviderLocal:

		if models, err := lmStudioEmbeddingModels(ctx, cfg.BaseURL); err == nil && len(models) > 0 {
			return models, nil
		}
		apiKey, err := s.decryptAPIKey(ctx, providerType)
		if err != nil {
			return nil, err
		}
		models, err := openAICompatibleModels(ctx, cfg.BaseURL, apiKey)
		if err != nil {
			return nil, err
		}
		return preferEmbeddingModels(models), nil
	case domain.LLMProviderGemini:

		return []string{"gemini-embedding-001"}, nil
	case domain.LLMProviderOpenAI:
		return []string{"text-embedding-3-small", "text-embedding-3-large", "text-embedding-ada-002"}, nil
	default:
		return nil, fmt.Errorf("this provider cannot produce embeddings")
	}
}

func preferEmbeddingModels(models []string) []string {
	embedding := make([]string, 0, len(models))
	for _, m := range models {
		if strings.Contains(strings.ToLower(m), "embed") {
			embedding = append(embedding, m)
		}
	}
	if len(embedding) == 0 {
		return models
	}
	return embedding
}

func lmStudioEmbeddingModels(ctx context.Context, baseURL string) ([]string, error) {
	host := strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
	url := host + "/api/v0/models"
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list LM Studio models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LM Studio /api/v0/models returned status %d", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]string, 0)
	for _, m := range payload.Data {
		if m.Type == "embeddings" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

func (s *Service) SetEmbedding(ctx context.Context, providerType domain.LLMProviderType, model string) (domain.LLMProvidersResponse, error) {
	if providerType != "" {
		if domain.ValidLLMProviderType(string(providerType)) {
			if !domain.ProviderAvailable(providerType) {
				return domain.LLMProvidersResponse{}, domain.ErrUnavailableProvider(providerType)
			}
			if providerType == domain.LLMProviderGroq {
				return domain.LLMProvidersResponse{}, fmt.Errorf("%s cannot produce embeddings; pick a different provider for embeddings", providerType)
			}

			if domain.RequiresHostExecutor(providerType) {
				return domain.LLMProvidersResponse{}, fmt.Errorf("%s cannot produce embeddings; pick a different provider for embeddings", providerType)
			}
		} else if s.endpoints != nil && endpointRef(providerType) {
			ep, err := s.endpoints.Get(ctx, string(providerType))
			if err != nil {
				return domain.LLMProvidersResponse{}, fmt.Errorf("invalid provider ref: %s", providerType)
			}
			if !ep.Configured {
				return domain.LLMProvidersResponse{}, fmt.Errorf("endpoint is not configured")
			}
		} else {
			return domain.LLMProvidersResponse{}, fmt.Errorf("invalid provider type: %s", providerType)
		}
	}
	if err := s.store.SetEmbeddingProvider(ctx, providerType); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if err := s.store.SetEmbeddingModel(ctx, model); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	return s.List(ctx)
}

func (s *Service) BootstrapFromYAML(ctx context.Context, yamlCfg domain.LLMConfig) error {
	active, err := s.store.GetActiveProvider(ctx)
	if err != nil {
		return err
	}
	cfg, err := s.store.Get(ctx, active)
	if err != nil {
		return err
	}
	if cfg.Configured {
		return s.reloadAllConfigured(ctx)
	}
	if yamlCfg.BaseURL == "" {
		return nil
	}
	timeoutSeconds := int(yamlCfg.Timeout.Seconds())
	if timeoutSeconds <= 0 {
		timeoutSeconds = resolveTimeoutSeconds(0, 0, domain.LLMProviderLocal)
	}
	localCfg := domain.LLMProviderConfig{
		ProviderType:   domain.LLMProviderLocal,
		BaseURL:        yamlCfg.BaseURL,
		DefaultModel:   yamlCfg.Model,
		TimeoutSeconds: timeoutSeconds,
		Configured:     true,
	}
	if err := s.store.Upsert(ctx, localCfg); err != nil {
		return err
	}
	if yamlCfg.APIKey != "" && s.cipher != nil {
		encrypted, err := s.cipher.Encrypt(yamlCfg.APIKey)
		if err != nil {
			return err
		}
		if err := s.store.SetAPIKey(ctx, domain.LLMProviderLocal, encrypted); err != nil {
			return err
		}
	}
	if err := s.store.SetActiveProvider(ctx, domain.LLMProviderLocal); err != nil {
		return err
	}
	return s.reloadAllConfigured(ctx)
}

func (s *Service) resolveAPIKey(incoming string, hasStored bool, ctx context.Context, providerType domain.LLMProviderType) string {
	if incoming != "" {
		return incoming
	}
	if !hasStored {
		return ""
	}
	key, err := s.decryptAPIKey(ctx, providerType)
	if err != nil {
		return ""
	}
	return key
}

func (s *Service) decryptAPIKey(ctx context.Context, providerType domain.LLMProviderType) (string, error) {
	encrypted, err := s.store.GetAPIKeyEncrypted(ctx, providerType)
	if err != nil {
		return "", err
	}
	if len(encrypted) == 0 {
		return "", nil
	}
	if s.cipher == nil {
		return "", fmt.Errorf("secret storage unavailable")
	}
	return s.cipher.Decrypt(encrypted)
}

func (s *Service) testClient(providerType domain.LLMProviderType, baseURL, defaultModel, apiKey string, timeoutSeconds int) error {
	timeout := time.Duration(timeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = s.timeout
	}
	client := llm.NewProviderClient(providerType, baseURL, defaultModel, apiKey, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := client.Models(ctx)
	return err
}

const endpointProviderRef = domain.LLMProviderType("endpoint")

func (s *Service) decryptEndpointAPIKey(ctx context.Context, id string) (string, error) {
	if s.endpoints == nil {
		return "", nil
	}
	encrypted, err := s.endpoints.GetAPIKeyEncrypted(ctx, id)
	if err != nil {
		return "", err
	}
	if len(encrypted) == 0 {
		return "", nil
	}
	if s.cipher == nil {
		return "", fmt.Errorf("secret storage unavailable")
	}
	return s.cipher.Decrypt(encrypted)
}

func normalizeEndpointTimeout(seconds int) (int, error) {
	if seconds <= 0 {
		return 120, nil
	}
	if seconds < 30 {
		return 0, fmt.Errorf("timeout_seconds must be at least 30")
	}
	if seconds > 3600 {
		return 0, fmt.Errorf("timeout_seconds must be at most 3600")
	}
	return seconds, nil
}

func (s *Service) CreateEndpoint(ctx context.Context, req domain.SaveLLMEndpointRequest) (domain.LLMProvidersResponse, error) {
	if s.endpoints == nil {
		return domain.LLMProvidersResponse{}, fmt.Errorf("endpoint storage unavailable")
	}
	name := strings.TrimSpace(req.Name)
	baseURL := strings.TrimSpace(req.BaseURL)
	if name == "" {
		return domain.LLMProvidersResponse{}, fmt.Errorf("name is required")
	}
	if baseURL == "" {
		return domain.LLMProvidersResponse{}, fmt.Errorf("base_url is required")
	}
	timeoutSeconds, err := normalizeEndpointTimeout(req.TimeoutSeconds)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	apiKey := strings.TrimSpace(req.APIKey)
	defaultModel := strings.TrimSpace(req.DefaultModel)

	if err := s.testClient(endpointProviderRef, baseURL, defaultModel, apiKey, timeoutSeconds); err != nil {
		return domain.LLMProvidersResponse{}, fmt.Errorf("connection test failed: %w", err)
	}

	created, err := s.endpoints.Create(ctx, domain.LLMEndpoint{
		Name:           name,
		BaseURL:        baseURL,
		DefaultModel:   defaultModel,
		TimeoutSeconds: timeoutSeconds,
		Configured:     true,
	})
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if apiKey != "" {
		if s.cipher == nil {
			return domain.LLMProvidersResponse{}, fmt.Errorf("secret storage unavailable")
		}
		encrypted, err := s.cipher.Encrypt(apiKey)
		if err != nil {
			return domain.LLMProvidersResponse{}, err
		}
		if err := s.endpoints.SetAPIKey(ctx, created.ID, encrypted); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	return s.List(ctx)
}

func (s *Service) UpdateEndpoint(ctx context.Context, id string, req domain.SaveLLMEndpointRequest) (domain.LLMProvidersResponse, error) {
	if s.endpoints == nil {
		return domain.LLMProvidersResponse{}, fmt.Errorf("endpoint storage unavailable")
	}
	if !endpointRef(domain.LLMProviderType(id)) {
		return domain.LLMProvidersResponse{}, errNoSuchEndpoint(id)
	}
	existing, err := s.endpoints.Get(ctx, id)
	if err != nil {
		return domain.LLMProvidersResponse{}, errNoSuchEndpoint(id)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = existing.Name
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		baseURL = existing.BaseURL
	}
	defaultModel := strings.TrimSpace(req.DefaultModel)
	if defaultModel == "" {
		defaultModel = existing.DefaultModel
	}
	timeoutSeconds := req.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = existing.TimeoutSeconds
	}
	timeoutSeconds, err = normalizeEndpointTimeout(timeoutSeconds)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	apiKey := strings.TrimSpace(req.APIKey)
	testKey := apiKey
	if testKey == "" && existing.HasAPIKey {
		testKey, _ = s.decryptEndpointAPIKey(ctx, id)
	}

	if err := s.testClient(domain.LLMProviderType(id), baseURL, defaultModel, testKey, timeoutSeconds); err != nil {
		return domain.LLMProvidersResponse{}, fmt.Errorf("connection test failed: %w", err)
	}

	if err := s.endpoints.Update(ctx, domain.LLMEndpoint{
		ID:             id,
		Name:           name,
		BaseURL:        baseURL,
		DefaultModel:   defaultModel,
		TimeoutSeconds: timeoutSeconds,
		Configured:     true,
	}); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if apiKey != "" {
		if s.cipher == nil {
			return domain.LLMProvidersResponse{}, fmt.Errorf("secret storage unavailable")
		}
		encrypted, err := s.cipher.Encrypt(apiKey)
		if err != nil {
			return domain.LLMProvidersResponse{}, err
		}
		if err := s.endpoints.SetAPIKey(ctx, id, encrypted); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	return s.List(ctx)
}

func (s *Service) DeleteEndpoint(ctx context.Context, id string) (domain.LLMProvidersResponse, error) {
	if s.endpoints == nil {
		return domain.LLMProvidersResponse{}, fmt.Errorf("endpoint storage unavailable")
	}
	if !endpointRef(domain.LLMProviderType(id)) {
		return domain.LLMProvidersResponse{}, errNoSuchEndpoint(id)
	}
	if err := s.endpoints.Delete(ctx, id); err != nil {
		return domain.LLMProvidersResponse{}, errNoSuchEndpoint(id)
	}
	if active, err := s.store.GetActiveProvider(ctx); err == nil && string(active) == id {
		if err := s.store.SetActiveProvider(ctx, domain.LLMProviderLocal); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	}
	if embedding, err := s.store.GetEmbeddingProvider(ctx); err == nil && string(embedding) == id {
		if err := s.store.SetEmbeddingProvider(ctx, ""); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	return s.List(ctx)
}

func (s *Service) TestEndpoint(ctx context.Context, id string, req domain.SaveLLMEndpointRequest) error {
	baseURL := strings.TrimSpace(req.BaseURL)
	defaultModel := strings.TrimSpace(req.DefaultModel)
	apiKey := strings.TrimSpace(req.APIKey)
	timeoutSeconds := req.TimeoutSeconds
	ref := endpointProviderRef
	if id != "" && s.endpoints != nil {
		if existing, err := s.endpoints.Get(ctx, id); err == nil {
			ref = domain.LLMProviderType(id)
			if baseURL == "" {
				baseURL = existing.BaseURL
			}
			if defaultModel == "" {
				defaultModel = existing.DefaultModel
			}
			if timeoutSeconds <= 0 {
				timeoutSeconds = existing.TimeoutSeconds
			}
			if apiKey == "" && existing.HasAPIKey {
				apiKey, _ = s.decryptEndpointAPIKey(ctx, id)
			}
		}
	}
	if baseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	timeoutSeconds, err := normalizeEndpointTimeout(timeoutSeconds)
	if err != nil {
		return err
	}
	return s.testClient(ref, baseURL, defaultModel, apiKey, timeoutSeconds)
}

func openAICompatibleModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/models"
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, providerErrorBodyLimit))
		return nil, newProviderError("/models", resp.StatusCode, providerErrorMessage(body))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		out = append(out, m.ID)
	}
	return out, nil
}
