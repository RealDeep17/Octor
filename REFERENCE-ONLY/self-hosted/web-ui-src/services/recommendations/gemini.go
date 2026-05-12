package recommendations

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"google.golang.org/genai"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
)

const (
	geminiTimeout             = 45 * time.Second
	geminiMaxTokensRecommend  = 4096
	geminiMaxTokensChips      = 1024
)

type GeminiService struct {
	cfg           Config
	client        *genai.Client
	context       *UserContextBuilder
	resolver      *Resolver
	quota         Quota
	chipsCache    *RedisChipsCache
	freshReleases *DBFreshReleasesLoader
}

func NewGeminiService(cfg Config, contextBuilder *UserContextBuilder, resolver *Resolver, quota Quota, chipsCache *RedisChipsCache, freshReleases *DBFreshReleasesLoader) *GeminiService {
	if !cfg.Enabled || cfg.GeminiAPIKey == "" {
		return nil
	}
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  cfg.GeminiAPIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		log.WithError(err).Error("failed to create gemini client")
		return nil
	}
	return &GeminiService{
		cfg:           cfg,
		client:        client,
		context:       contextBuilder,
		resolver:      resolver,
		quota:         quota,
		chipsCache:    chipsCache,
		freshReleases: freshReleases,
	}
}

func (s *GeminiService) RecommendStream(ctx context.Context, req RecommendRequest, events chan<- StreamEvent) {
	defer close(events)

	// 1. Quota check
	remaining, err := s.quota.Consume(ctx, req.UserID, req.Tier)
	if err != nil {
		events <- StreamEvent{Type: "error", Data: ErrorStreamPayload{
			Code:    "quota_exceeded",
			Tier:    req.Tier.String(),
			ResetAt: s.quota.ResetAt().Unix(),
		}}
		return
	}

	// 2. Build context
	uc, err := s.context.Build(ctx, req.UserID, req.Locale, req.Clock)
	if err != nil {
		log.WithError(err).WithField("feature", "ai_rec").Warn("context build failed (continuing with cold start)")
	}

	// 3. Start streaming
	events <- StreamEvent{Type: "phase", Data: PhaseStreamPayload{Phase: "gemini"}}

	itemsChan := make(chan claudeItem, 8)
	recsChan := make(chan Recommendation, 8)

	// Start resolver in background
	// We assume ContentTypeMovie for now (MVP) as per Service.RecommendStream contract.
	go s.resolver.ResolveStreamFromChannel(ctx, itemsChan, models.ContentTypeMovie, req.Locale, recsChan)

	// Start Gemini in background
	modelName := s.cfg.ResolveModel(req.Tier)
	log.WithFields(log.Fields{
		"feature": "ai_rec",
		"model":   modelName,
		"tier":    req.Tier.String(),
	}).Info("starting gemini recommendation")

	var streamErr error
	go func() {
		prompt := userPromptForRecommend(uc, req.Query, 6, 8)
		streamErr = s.streamGeminiItemsText(ctx, prompt, req.History, req.Tier, itemsChan)
	}()

	// 4. Resolve items as they arrive
	total := 0
	events <- StreamEvent{Type: "phase", Data: PhaseStreamPayload{Phase: "resolving", Expected: 8}}

	for rec := range recsChan {
		events <- StreamEvent{Type: "item", Data: rec}
		total++
	}

	if streamErr != nil {
		log.WithError(streamErr).WithField("feature", "ai_rec").Error("gemini stream failed")
		events <- StreamEvent{Type: "error", Data: ErrorStreamPayload{Code: "internal"}}
		return
	}

	// 5. Done
	events <- StreamEvent{Type: "phase", Data: PhaseStreamPayload{Phase: "done"}}
	events <- StreamEvent{Type: "done", Data: DoneStreamPayload{
		Total:          total,
		RemainingQuota: remaining,
		DailyQuota:     s.DailyQuota(req.Tier),
		Tier:           req.Tier.String(),
	}}
}

func (s *GeminiService) streamGeminiItemsText(ctx context.Context, userPrompt string, history []Message, tier Tier, out chan<- claudeItem) error {
	defer close(out)

	ctx, cancel := context.WithTimeout(ctx, geminiTimeout)
	defer cancel()

	modelName := s.cfg.ResolveModel(tier)
	
	// System prompt
	systemInstruction := systemPromptNDJSON
	if s.freshReleases != nil {
		if block := s.freshReleases.LoadFreshReleases(ctx); block != "" {
			systemInstruction += "\n\n# RECENT RELEASES\n\n" + block
		}
	}

	// Build contents from history + current prompt
	var contents []*genai.Content
	for _, msg := range history {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, &genai.Content{
			Role:  role,
			Parts: []*genai.Part{{Text: msg.Content}},
		})
	}
	contents = append(contents, &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: userPrompt}},
	})

	// Configure tools (Google Search Grounding)
	var tools []*genai.Tool
	if s.cfg.GoogleSearch {
		tools = append(tools, &genai.Tool{
			GoogleSearch: &genai.GoogleSearch{},
		})
	}

	iter := s.client.Models.GenerateContentStream(ctx, modelName, contents, &genai.GenerateContentConfig{
		Temperature:       ptr(float32(0.7)),
		MaxOutputTokens:   ptr(int32(geminiMaxTokensRecommend)),
		SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: systemInstruction}}},
		Tools:             tools,
	})

	extractor := newNDJSONItemsExtractor(func(raw json.RawMessage) {
		var item claudeItem
		if err := json.Unmarshal(raw, &item); err != nil {
			log.WithError(err).WithField("feature", "ai_rec").Warn("ndjson item parse failed")
			return
		}
		select {
		case out <- item:
		case <-ctx.Done():
		}
	})

	for resp, err := range iter {
		if err != nil {
			return err
		}
		for _, cand := range resp.Candidates {
			if cand.Content != nil {
				for _, part := range cand.Content.Parts {
					if part.Text != "" {
						extractor.write(part.Text)
					}
				}
			}
		}
	}
	return nil
}

func (s *GeminiService) GenerateChips(ctx context.Context, req ChipsRequest) (*ChipsResponse, error) {
	// 1. Cache hit?
	if !req.ForceRefresh {
		if cached, err := s.chipsCache.Get(ctx, req.UserID.String()); err == nil && cached != nil {
			return cached, nil
		}
	}

	// 2. Build context
	uc, err := s.context.Build(ctx, req.UserID, req.Locale, req.Clock)
	if err != nil {
		log.WithError(err).WithField("feature", "ai_rec").Warn("context build failed (continuing with cold start)")
	}

	modelName := s.cfg.ResolveChipsModel()
	prompt := userPromptForChips(uc, 6)

	// Configure tools (Function Calling for Chips)
	tools := []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "return_chips",
					Description: "Returns a list of recommendation chips.",
					Parameters: &genai.Schema{
						Type:     genai.TypeObject,
						Required: []string{"chips"},
						Properties: map[string]*genai.Schema{
							"chips": {
								Type: genai.TypeArray,
								Items: &genai.Schema{
									Type: genai.TypeObject,
									Required: []string{"id", "label", "query"},
									Properties: map[string]*genai.Schema{
										"id":    {Type: genai.TypeString},
										"label": {Type: genai.TypeString},
										"icon":  {Type: genai.TypeString},
										"query": {Type: genai.TypeString},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	resp, err := s.client.Models.GenerateContent(ctx, modelName, []*genai.Content{{Parts: []*genai.Part{{Text: prompt}}}}, &genai.GenerateContentConfig{
		Temperature:     ptr(float32(1.0)),
		MaxOutputTokens: ptr(int32(geminiMaxTokensChips)),
		Tools:           tools,
		ToolConfig: &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode:                 genai.FunctionCallingConfigModeAny,
				AllowedFunctionNames: []string{"return_chips"},
			},
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "gemini chip generation failed")
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil, errors.New("no candidates returned")
	}

	var chips []Chip
	for _, part := range resp.Candidates[0].Content.Parts {
		if part.FunctionCall != nil && part.FunctionCall.Name == "return_chips" {
			if rawChips, ok := part.FunctionCall.Args["chips"].([]any); ok {
				for _, rc := range rawChips {
					if m, ok := rc.(map[string]any); ok {
						chip := Chip{
							ID:    fmt.Sprint(m["id"]),
							Label: fmt.Sprint(m["label"]),
							Query: fmt.Sprint(m["query"]),
						}
						if icon, ok := m["icon"].(string); ok {
							chip.Icon = icon
						}
						chips = append(chips, chip)
					}
				}
			}
		} else if part.Text != "" && len(chips) == 0 {
			// Fallback: Some models ignore FunctionCalling
			// and return a JSON block in the text instead.
			content := part.Text
			start := strings.Index(content, "{")
			end := strings.LastIndex(content, "}")
			if start != -1 && end != -1 && end > start {
				var parsed struct {
					Chips []Chip `json:"chips"`
				}
				if err := json.Unmarshal([]byte(content[start:end+1]), &parsed); err == nil && len(parsed.Chips) > 0 {
					chips = parsed.Chips
				}
			}
		}
	}

	if len(chips) == 0 {
		return nil, ErrNoChips
	}

	res := &ChipsResponse{
		Chips:       chips,
		GeneratedAt: time.Now().Unix(),
		Tier:        req.Tier.String(),
	}

	// 4. Cache
	_ = s.chipsCache.Set(ctx, req.UserID.String(), res, time.Duration(s.cfg.ChipsTTLSeconds)*time.Second)

	return res, nil
}

func (s *GeminiService) Remaining(ctx context.Context, userID uuid.UUID, tier Tier) (int, error) {
	return s.quota.Remaining(ctx, userID, tier)
}

func (s *GeminiService) DailyQuota(tier Tier) int {
	if tier == TierPaid {
		return s.cfg.PaidDailyQuota
	}
	return s.cfg.FreeDailyQuota
}

func (s *GeminiService) QuotaResetAt() int64 {
	return s.quota.ResetAt().Unix()
}

func (s *GeminiService) ConsumeQuota(ctx context.Context, userID uuid.UUID, tier Tier) (int, error) {
	return s.quota.Consume(ctx, userID, tier)
}

func shortHash(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum32())
}

func ptr[T any](v T) *T {
	return &v
}
