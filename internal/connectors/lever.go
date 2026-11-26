package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"rolerelay/internal/domain"
)

const DefaultLeverBaseURL = "https://api.lever.co"

type LeverClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewLeverClient() *LeverClient {
	return &LeverClient{
		BaseURL: DefaultLeverBaseURL,
		HTTPClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

type leverPosting struct {
	ID               string `json:"id"`
	Text             string `json:"text"`
	HostedURL        string `json:"hostedUrl"`
	ApplyURL         string `json:"applyUrl"`
	Description      string `json:"description"`
	DescriptionPlain string `json:"descriptionPlain"`
	Additional       string `json:"additional"`
	AdditionalPlain  string `json:"additionalPlain"`
	WorkplaceType    string `json:"workplaceType"`
	Categories       struct {
		Location   string `json:"location"`
		Team       string `json:"team"`
		Commitment string `json:"commitment"`
	} `json:"categories"`
	Lists []struct {
		Text    string `json:"text"`
		Content string `json:"content"`
	} `json:"lists"`
}

func (c *LeverClient) FetchJobs(ctx context.Context, companyBoard, displayName string) ([]ImportedVacancy, error) {
	board := strings.TrimSpace(companyBoard)
	if board == "" {
		return nil, fmt.Errorf("lever company board is required")
	}

	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultLeverBaseURL
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}

	endpoint := fmt.Sprintf("%s/v0/postings/%s?mode=json", baseURL, url.PathEscape(board))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build lever request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "RoleRelay/0.2")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch lever board %q: %w", board, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("lever company board %q was not found (HTTP 404)", board)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("lever company board %q returned HTTP %d", board, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read lever response: %w", err)
	}

	var postings []leverPosting
	if err := json.Unmarshal(body, &postings); err != nil {
		return nil, fmt.Errorf("parse lever JSON: %w", err)
	}

	company := strings.TrimSpace(displayName)
	if company == "" {
		company = board
	}

	out := make([]ImportedVacancy, 0, len(postings))
	for _, p := range postings {
		rawJob, _ := json.Marshal(p)
		desc := buildLeverDescription(p)
		loc := strings.TrimSpace(p.Categories.Location)

		applyURL := strings.TrimSpace(p.ApplyURL)
		if applyURL == "" {
			applyURL = strings.TrimSpace(p.HostedURL)
		}
		canonical := CanonicalizeURL(strings.TrimSuffix(applyURL, "/apply"))
		if canonical == "" {
			canonical = CanonicalizeURL(p.HostedURL)
		}

		arrangement := mapLeverWorkplaceType(p.WorkplaceType)
		if arrangement == domain.WorkArrangementUnknown {
			arrangement = InferWorkArrangement(loc, desc)
		}

		out = append(out, ImportedVacancy{
			SourceType:       domain.ProviderLever,
			Provider:         string(domain.ProviderLever),
			CompanyBoard:     strings.ToLower(board),
			ExternalJobID:    strings.TrimSpace(p.ID),
			CanonicalURL:     canonical,
			CompanyName:      company,
			Title:            strings.TrimSpace(p.Text),
			Location:         loc,
			WorkArrangement:  arrangement,
			Description:      desc,
			ApplicationEmail: ExtractApplicationEmail(desc),
			ApplicationURL:   applyURL,
			RawJSON:          string(rawJob),
		})
	}
	return out, nil
}

func buildLeverDescription(p leverPosting) string {
	var sections []string
	intro := strings.TrimSpace(p.DescriptionPlain)
	if intro == "" {
		intro = HTMLToPlainText(p.Description)
	}
	if intro != "" {
		sections = append(sections, intro)
	}
	for _, list := range p.Lists {
		heading := strings.TrimSpace(list.Text)
		content := HTMLToPlainText(list.Content)
		if heading != "" && content != "" {
			sections = append(sections, heading+"\n"+content)
		} else if content != "" {
			sections = append(sections, content)
		}
	}
	extra := strings.TrimSpace(p.AdditionalPlain)
	if extra == "" {
		extra = HTMLToPlainText(p.Additional)
	}
	if extra != "" {
		sections = append(sections, extra)
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func mapLeverWorkplaceType(raw string) domain.WorkArrangement {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "remote":
		return domain.WorkArrangementRemote
	case "hybrid":
		return domain.WorkArrangementHybrid
	case "onsite", "on-site":
		return domain.WorkArrangementOnsite
	default:
		return domain.WorkArrangementUnknown
	}
}
