package evaluator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"rolerelay/internal/domain"
)

type OpenAICompatibleEvaluator struct {
	EndpointURL string
	Model       string
	APIKey      string
	HTTPClient  *http.Client
}

func NewOpenAICompatibleEvaluator(cfg domain.ModelSettings) *OpenAICompatibleEvaluator {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	return &OpenAICompatibleEvaluator{
		EndpointURL: strings.TrimRight(strings.TrimSpace(cfg.EndpointURL), "/"),
		Model:       strings.TrimSpace(cfg.Model),
		APIKey:      strings.TrimSpace(cfg.APIKey),
		HTTPClient:  &http.Client{Timeout: timeout},
	}
}

func (e *OpenAICompatibleEvaluator) Name() string {
	return "openai-compatible:" + e.Model
}

type chatCompletionRequest struct {
	Model          string           `json:"model"`
	Temperature    float64          `json:"temperature"`
	ResponseFormat *responseFormat  `json:"response_format,omitempty"`
	Messages       []chatMessage    `json:"messages"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type structuredEvalPayload struct {
	FitScore       int                         `json:"fitScore"`
	Summary        string                      `json:"summary"`
	Findings       []domain.RequirementFinding `json:"findings"`
	MissingAnswers []domain.MissingAnswer      `json:"missingAnswers"`
}

func (e *OpenAICompatibleEvaluator) Evaluate(ctx context.Context, input ModelInput) (ModelOutput, error) {
	if e.EndpointURL == "" || e.Model == "" {
		return ModelOutput{}, fmt.Errorf("model endpoint URL and model name are required")
	}

	var cvBlock strings.Builder
	for _, cv := range input.CVs {
		cvBlock.WriteString(fmt.Sprintf("=== CV (%s, v%d, %s) ===\n%s\n\n", strings.ToUpper(string(cv.Language)), cv.Version, cv.Filename, cv.ExtractedText))
	}

	systemPrompt := `You evaluate job vacancy fit against a candidate's CV(s).
Return ONLY valid JSON with this exact shape:
{
  "fitScore": 0-100,
  "summary": "short explanation",
  "findings": [
    {
      "requirement": "requirement text",
      "kind": "mandatory" | "preference",
      "met": true | false,
      "evidenceExcerpt": "EXACT verbatim substring copied from the provided CV text when met=true, else empty string",
      "cvLanguage": "en" | "es",
      "reason": "brief explanation"
    }
  ],
  "missingAnswers": [
    {
      "key": "salary_expectation",
      "label": "Expected salary",
      "kind": "salary" | "text" | "select",
      "required": true,
      "options": [],
      "reason": "why this answer is needed"
    }
  ]
}
Rules:
- Distinguish mandatory requirements from preferences.
- When met=true, evidenceExcerpt MUST be an exact verbatim substring from the provided CV text.
- Never fabricate CV evidence; if evidence is missing, set met=false and evidenceExcerpt="".`

	userPrompt := fmt.Sprintf(
		"Candidate target roles: %s\nCandidate location: %s\nCandidate work arrangement: %s\n\n=== VACANCY ===\nCompany: %s\nTitle: %s\nLocation: %s\nWork Arrangement: %s\nDescription:\n%s\n\n%s",
		strings.Join(input.Profile.TargetRoles, ", "),
		input.Profile.Location,
		input.Profile.WorkArrangement,
		input.Vacancy.CompanyName,
		input.Vacancy.Title,
		input.Vacancy.Location,
		input.Vacancy.WorkArrangement,
		input.Vacancy.Description,
		cvBlock.String(),
	)

	reqPayload := chatCompletionRequest{
		Model:          e.Model,
		Temperature:    0,
		ResponseFormat: &responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return ModelOutput{}, err
	}

	endpoint := e.EndpointURL
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		endpoint += "/chat/completions"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return ModelOutput{}, fmt.Errorf("build model request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.APIKey)
	}

	client := e.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ModelOutput{}, fmt.Errorf("call model endpoint: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return ModelOutput{}, fmt.Errorf("read model response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ModelOutput{}, fmt.Errorf("model endpoint returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	var completion chatCompletionResponse
	if err := json.Unmarshal(respBytes, &completion); err != nil {
		return ModelOutput{}, fmt.Errorf("decode model completion response: %w", err)
	}
	if len(completion.Choices) == 0 {
		return ModelOutput{}, fmt.Errorf("model endpoint returned zero choices")
	}

	rawContent := extractJSONObject(completion.Choices[0].Message.Content)
	var parsed structuredEvalPayload
	if err := json.Unmarshal([]byte(rawContent), &parsed); err != nil {
		return ModelOutput{}, fmt.Errorf("parse structured evaluation JSON: %w", err)
	}

	if parsed.FitScore < 0 {
		parsed.FitScore = 0
	}
	if parsed.FitScore > 100 {
		parsed.FitScore = 100
	}
	return ModelOutput{
		ModelName:      e.Name(),
		FitScore:       parsed.FitScore,
		Summary:        strings.TrimSpace(parsed.Summary),
		Findings:       parsed.Findings,
		MissingAnswers: parsed.MissingAnswers,
	}, nil
}

func extractJSONObject(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		if end := strings.LastIndex(s, "```"); end >= 0 {
			s = s[:end]
		}
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return strings.TrimSpace(s)
}
