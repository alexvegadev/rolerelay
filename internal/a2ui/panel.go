package a2ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"rolerelay/internal/domain"
)

const (
	ProtocolVersion = "1.0"
	CatalogID       = "https://rolerelay.local/a2ui/v1/review-catalog.json"
)

var SupportedCurrencies = []string{"USD", "EUR", "ARS", "MXN", "COP", "GBP"}

type AgentMessage struct {
	Version       string         `json:"version"`
	CreateSurface *CreateSurface `json:"createSurface,omitempty"`
}

type CreateSurface struct {
	SurfaceID     string           `json:"surfaceId"`
	CatalogID     string           `json:"catalogId"`
	SendDataModel bool             `json:"sendDataModel"`
	Components    []map[string]any `json:"components"`
	DataModel     map[string]any   `json:"dataModel"`
}

type ActionEnvelope struct {
	Version string        `json:"version"`
	Action  ActionPayload `json:"action"`
}

type ActionPayload struct {
	Name              string         `json:"name"`
	SurfaceID         string         `json:"surfaceId"`
	SourceComponentID string         `json:"sourceComponentId"`
	Timestamp         string         `json:"timestamp"`
	Context           map[string]any `json:"context"`
}

type PanelResponse struct {
	SurfaceID   string         `json:"surfaceId"`
	CatalogID   string         `json:"catalogId"`
	ReviewCases []string       `json:"reviewCases"`
	HasInputs   bool           `json:"hasInputs"`
	Messages    []AgentMessage `json:"messages"`
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

func BuildPendingReviewPanel(
	detail domain.ApplicationDetail,
	profile domain.Profile,
	cvs []domain.CVDocument,
) PanelResponse {
	surfaceID := fmt.Sprintf("pending_review_app_%d", detail.Application.ID)
	components := make([]map[string]any, 0, 16)
	rootChildren := make([]string, 0, 10)
	answersModel := map[string]any{}
	actionContext := map[string]any{
		"applicationId": detail.Application.ID,
	}
	reviewCases := make([]string, 0, 4)

	var missingAnswers []domain.MissingAnswer
	var findings []domain.RequirementFinding
	if detail.LatestEvaluation != nil {
		missingAnswers = detail.LatestEvaluation.MissingAnswers
		findings = detail.LatestEvaluation.Findings
	}

	for _, m := range missingAnswers {
		reviewCases = append(reviewCases, m.Kind)
	}

	bannerReason := detail.Application.PendingReason
	if bannerReason == "" && detail.LatestEvaluation != nil {
		bannerReason = detail.LatestEvaluation.Summary
	}
	if bannerReason == "" {
		bannerReason = "Review the pending requirements and submit any missing information."
	}

	components = append(components, map[string]any{
		"id":          "review_banner",
		"component":   "MissingAnswerBanner",
		"title":       fmt.Sprintf("Review required: %s", detail.Vacancy.Title),
		"description": bannerReason,
		"cases":       reviewCases,
	})
	rootChildren = append(rootChildren, "review_banner")

	hasInputs := false
	for idx, m := range missingAnswers {
		switch m.Kind {
		case "salary":
			hasInputs = true
			existingAmt := ""
			existingCur := "USD"
			if prev, ok := detail.Application.A2UIAnswers[m.Key].(map[string]any); ok {
				if amt, ok := prev["amount"]; ok {
					existingAmt = fmt.Sprintf("%v", amt)
				}
				if cur, ok := prev["currency"].(string); ok && cur != "" {
					existingCur = cur
				}
			}
			answersModel["salaryAmount"] = existingAmt
			answersModel["salaryCurrency"] = []string{existingCur}

			amountID := fmt.Sprintf("salary_amount_%d", idx)
			currencyID := fmt.Sprintf("salary_currency_%d", idx)
			rowID := fmt.Sprintf("salary_row_%d", idx)
			hintID := fmt.Sprintf("salary_hint_%d", idx)

			components = append(components,
				map[string]any{
					"id":        hintID,
					"component": "Text",
					"text":      fallbackText(m.Reason, "Enter your expected salary amount and currency."),
					"variant":   "caption",
				},
				map[string]any{
					"id":          amountID,
					"component":   "TextField",
					"label":       "Expected salary amount",
					"placeholder": "e.g. 95000",
					"variant":     "number",
					"value":       map[string]any{"@path": "/answers/salaryAmount"},
					"weight":      1,
					"@checks": []map[string]any{
						{
							"condition": map[string]any{
								"@call": "required",
								"args":  map[string]any{"value": map[string]any{"@path": "/answers/salaryAmount"}},
							},
							"message": "Enter a positive salary amount.",
						},
					},
				},
				map[string]any{
					"id":           currencyID,
					"component":    "ChoicePicker",
					"label":        "Currency",
					"variant":      "mutuallyExclusive",
					"displayStyle": "chips",
					"options":      currencyOptions(),
					"value":        map[string]any{"@path": "/answers/salaryCurrency"},
					"weight":       1,
				},
				map[string]any{
					"id":        rowID,
					"component": "Row",
					"children":  []string{amountID, currencyID},
					"align":     "start",
				},
			)
			rootChildren = append(rootChildren, hintID, rowID)
			actionContext["salaryAmount"] = map[string]any{"@path": "/answers/salaryAmount"}
			actionContext["salaryCurrency"] = map[string]any{"@path": "/answers/salaryCurrency"}

		case "cv_language":
			hasInputs = true
			options := availableCVLanguageOptions(profile, cvs)
			selected := []string{}
			if prev, ok := detail.Application.A2UIAnswers["cv_language"].(string); ok && prev != "" {
				selected = []string{prev}
			} else if len(options) == 1 {
				if v, ok := options[0]["value"].(string); ok {
					selected = []string{v}
				}
			}
			answersModel["cvLanguage"] = selected

			pickerID := fmt.Sprintf("cv_language_picker_%d", idx)
			hintID := fmt.Sprintf("cv_language_hint_%d", idx)
			components = append(components,
				map[string]any{
					"id":        hintID,
					"component": "Text",
					"text":      fallbackText(m.Reason, "Select which CV language RoleRelay should evaluate and attach for this vacancy."),
					"variant":   "caption",
				},
				map[string]any{
					"id":           pickerID,
					"component":    "ChoicePicker",
					"label":        "CV language for this application",
					"variant":      "mutuallyExclusive",
					"displayStyle": "chips",
					"options":      options,
					"value":        map[string]any{"@path": "/answers/cvLanguage"},
				},
			)
			rootChildren = append(rootChildren, hintID, pickerID)
			actionContext["cvLanguage"] = map[string]any{"@path": "/answers/cvLanguage"}

		default:
			hasInputs = true
			fieldKey := sanitizePathKey(m.Key)
			existing := ""
			if prev, ok := detail.Application.A2UIAnswers[m.Key]; ok {
				existing = fmt.Sprintf("%v", prev)
			}
			answersModel[fieldKey] = existing
			fieldID := fmt.Sprintf("answer_field_%d", idx)
			components = append(components, map[string]any{
				"id":          fieldID,
				"component":   "TextField",
				"label":       fallbackText(m.Label, m.Key),
				"placeholder": fallbackText(m.Reason, "Enter your answer"),
				"variant":     "shortText",
				"value":       map[string]any{"@path": "/answers/" + fieldKey},
			})
			rootChildren = append(rootChildren, fieldID)
			actionContext[fieldKey] = map[string]any{"@path": "/answers/" + fieldKey}
		}
	}

	if hasInputs {
		components = append(components,
			map[string]any{
				"id":        "submit_btn_text",
				"component": "Text",
				"text":      "Save & Reevaluate",
			},
			map[string]any{
				"id":        "submit_review_btn",
				"component": "Button",
				"child":     "submit_btn_text",
				"variant":   "primary",
				"action": map[string]any{
					"event": map[string]any{
						"name":    "save_review_answers",
						"context": actionContext,
					},
				},
			},
		)
		rootChildren = append(rootChildren, "submit_review_btn")
	}

	if len(findings) > 0 {
		components = append(components, map[string]any{
			"id":        "evidence_divider",
			"component": "Divider",
			"axis":      "horizontal",
		})
		rootChildren = append(rootChildren, "evidence_divider")

		for idx, f := range findings {
			cardID := fmt.Sprintf("evidence_item_%d", idx)
			components = append(components, map[string]any{
				"id":               cardID,
				"component":        "RequirementEvidenceCard",
				"requirement":      f.Requirement,
				"kind":             string(f.Kind),
				"met":              f.Met,
				"evidenceVerified": f.EvidenceVerified,
				"evidenceExcerpt":  f.EvidenceExcerpt,
				"reason":           f.Reason,
			})
			rootChildren = append(rootChildren, cardID)
		}
	}

	// Root component must have id "root" in A2UI v1.0 surfaces.
	rootComponent := map[string]any{
		"id":        "root",
		"component": "Column",
		"children":  rootChildren,
		"align":     "stretch",
	}
	allComponents := append([]map[string]any{rootComponent}, components...)

	msg := AgentMessage{
		Version: ProtocolVersion,
		CreateSurface: &CreateSurface{
			SurfaceID:     surfaceID,
			CatalogID:     CatalogID,
			SendDataModel: true,
			Components:    allComponents,
			DataModel: map[string]any{
				"answers": answersModel,
			},
		},
	}

	return PanelResponse{
		SurfaceID:   surfaceID,
		CatalogID:   CatalogID,
		ReviewCases: reviewCases,
		HasInputs:   hasInputs,
		Messages:    []AgentMessage{msg},
	}
}

func ValidateAndExtractAnswers(
	action ActionPayload,
	detail domain.ApplicationDetail,
	profile domain.Profile,
	cvs []domain.CVDocument,
) (map[string]any, string, error) {
	if action.Name != "save_review_answers" {
		return nil, "", &ValidationError{Field: "action", Message: fmt.Sprintf("unsupported A2UI action %q", action.Name)}
	}
	if detail.LatestEvaluation == nil {
		return nil, "", &ValidationError{Message: "application has no evaluation to review"}
	}

	merged := make(map[string]any, len(detail.Application.A2UIAnswers)+2)
	for k, v := range detail.Application.A2UIAnswers {
		merged[k] = v
	}

	var summaryParts []string
	for _, m := range detail.LatestEvaluation.MissingAnswers {
		switch m.Kind {
		case "salary":
			rawAmt := action.Context["salaryAmount"]
			amount, err := parsePositiveAmount(rawAmt)
			if err != nil {
				return nil, "", &ValidationError{Field: "salaryAmount", Message: err.Error()}
			}
			currency := strings.ToUpper(strings.TrimSpace(extractSingleChoice(action.Context["salaryCurrency"])))
			if !isSupportedCurrency(currency) {
				return nil, "", &ValidationError{
					Field:   "salaryCurrency",
					Message: fmt.Sprintf("select a supported currency (%s)", strings.Join(SupportedCurrencies, ", ")),
				}
			}
			merged[m.Key] = map[string]any{
				"amount":   amount,
				"currency": currency,
			}
			summaryParts = append(summaryParts, fmt.Sprintf("salary expectation %s %.0f", currency, amount))

		case "cv_language":
			choice := strings.ToLower(strings.TrimSpace(extractSingleChoice(action.Context["cvLanguage"])))
			lang := domain.CVLanguage(choice)
			if !lang.Valid() {
				return nil, "", &ValidationError{Field: "cvLanguage", Message: "select English (en) or Spanish (es) CV"}
			}
			if profile.LanguageMode != domain.LanguageModeBoth && string(profile.LanguageMode) != string(lang) {
				return nil, "", &ValidationError{
					Field:   "cvLanguage",
					Message: fmt.Sprintf("language %q is outside your configured profile mode %q", lang, profile.LanguageMode),
				}
			}
			if !hasActiveCVForLanguage(cvs, lang) {
				return nil, "", &ValidationError{
					Field:   "cvLanguage",
					Message: fmt.Sprintf("no active %s PDF CV is uploaded", strings.ToUpper(string(lang))),
				}
			}
			merged["cv_language"] = string(lang)
			summaryParts = append(summaryParts, fmt.Sprintf("CV language %s", strings.ToUpper(string(lang))))

		default:
			fieldKey := sanitizePathKey(m.Key)
			val := strings.TrimSpace(fmt.Sprintf("%v", action.Context[fieldKey]))
			if action.Context[fieldKey] == nil || val == "" {
				if m.Required {
					return nil, "", &ValidationError{Field: fieldKey, Message: fmt.Sprintf("%s is required", fallbackText(m.Label, m.Key))}
				}
				continue
			}
			merged[m.Key] = val
			summaryParts = append(summaryParts, fmt.Sprintf("%s recorded", fallbackText(m.Label, m.Key)))
		}
	}

	if len(summaryParts) == 0 {
		return nil, "", &ValidationError{Message: "no pending A2UI questions require an answer"}
	}

	summary := "A2UI review answer saved: " + strings.Join(summaryParts, "; ") + "."
	return merged, summary, nil
}

func currencyOptions() []map[string]any {
	opts := make([]map[string]any, 0, len(SupportedCurrencies))
	for _, cur := range SupportedCurrencies {
		opts = append(opts, map[string]any{
			"label": cur,
			"value": cur,
		})
	}
	return opts
}

func availableCVLanguageOptions(profile domain.Profile, cvs []domain.CVDocument) []map[string]any {
	activeByLang := map[domain.CVLanguage]domain.CVDocument{}
	for _, c := range cvs {
		if c.IsActive {
			activeByLang[c.Language] = c
		}
	}

	opts := make([]map[string]any, 0, 2)
	for _, lang := range profile.LanguageMode.RequiredCVLanguages() {
		label := "English CV (EN)"
		if lang == domain.CVLanguageES {
			label = "Spanish CV (ES)"
		}
		if doc, ok := activeByLang[lang]; ok {
			label = fmt.Sprintf("%s — %s v%d", label, doc.Filename, doc.Version)
		}
		opts = append(opts, map[string]any{
			"label": label,
			"value": string(lang),
		})
	}
	return opts
}

func hasActiveCVForLanguage(cvs []domain.CVDocument, lang domain.CVLanguage) bool {
	for _, c := range cvs {
		if c.IsActive && c.Language == lang {
			return true
		}
	}
	return false
}

func extractSingleChoice(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case []string:
		if len(v) > 0 {
			return v[0]
		}
	case []any:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return s
			}
			return fmt.Sprintf("%v", v[0])
		}
	}
	return ""
}

func parsePositiveAmount(raw any) (float64, error) {
	if raw == nil {
		return 0, fmt.Errorf("enter a positive salary amount")
	}
	var val float64
	switch v := raw.(type) {
	case float64:
		val = v
	case int:
		val = float64(v)
	case string:
		cleaned := strings.ReplaceAll(strings.TrimSpace(v), ",", "")
		if cleaned == "" {
			return 0, fmt.Errorf("enter a positive salary amount")
		}
		parsed, err := strconv.ParseFloat(cleaned, 64)
		if err != nil {
			return 0, fmt.Errorf("salary amount must be a valid number")
		}
		val = parsed
	default:
		return 0, fmt.Errorf("salary amount must be a valid number")
	}
	if math.IsNaN(val) || math.IsInf(val, 0) || val <= 0 {
		return 0, fmt.Errorf("salary amount must be greater than zero")
	}
	return val, nil
}

func isSupportedCurrency(cur string) bool {
	for _, c := range SupportedCurrencies {
		if c == cur {
			return true
		}
	}
	return false
}

func sanitizePathKey(key string) string {
	var b strings.Builder
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "answer"
	}
	return b.String()
}

func fallbackText(s, fallback string) string {
	if strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return fallback
}
