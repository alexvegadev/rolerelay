package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"rolerelay/internal/domain"
)

const DefaultGreenhouseBaseURL = "https://boards-api.greenhouse.io"

type GreenhouseClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewGreenhouseClient() *GreenhouseClient {
	return &GreenhouseClient{
		BaseURL: DefaultGreenhouseBaseURL,
		HTTPClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

type greenhouseResponse struct {
	Jobs []greenhouseJob `json:"jobs"`
}

type greenhouseJob struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	AbsoluteURL string `json:"absolute_url"`
	Content     string `json:"content"`
	Location    struct {
		Name string `json:"name"`
	} `json:"location"`
	Metadata []any `json:"metadata"`
}

func (c *GreenhouseClient) FetchJobs(ctx context.Context, companyBoard, displayName string) ([]ImportedVacancy, error) {
	board := strings.TrimSpace(companyBoard)
	if board == "" {
		return nil, fmt.Errorf("greenhouse board token is required")
	}

	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultGreenhouseBaseURL
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}

	endpoint := fmt.Sprintf("%s/v1/boards/%s/jobs?content=true", baseURL, url.PathEscape(board))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build greenhouse request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "RoleRelay/0.2")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch greenhouse board %q: %w", board, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("greenhouse board %q was not found (HTTP 404)", board)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("greenhouse board %q returned HTTP %d", board, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read greenhouse response: %w", err)
	}

	var parsed greenhouseResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse greenhouse JSON: %w", err)
	}

	company := strings.TrimSpace(displayName)
	if company == "" {
		company = board
	}

	out := make([]ImportedVacancy, 0, len(parsed.Jobs))
	for _, job := range parsed.Jobs {
		rawJob, _ := json.Marshal(job)
		desc := HTMLToPlainText(job.Content)
		loc := strings.TrimSpace(job.Location.Name)
		applyURL := strings.TrimSpace(job.AbsoluteURL)
		out = append(out, ImportedVacancy{
			SourceType:       domain.ProviderGreenhouse,
			Provider:         string(domain.ProviderGreenhouse),
			CompanyBoard:     strings.ToLower(board),
			ExternalJobID:    strconv.FormatInt(job.ID, 10),
			CanonicalURL:     CanonicalizeURL(applyURL),
			CompanyName:      company,
			Title:            strings.TrimSpace(job.Title),
			Location:         loc,
			WorkArrangement:  InferWorkArrangement(loc, desc),
			Description:      desc,
			ApplicationEmail: ExtractApplicationEmail(desc),
			ApplicationURL:   applyURL,
			RawJSON:          string(rawJob),
		})
	}
	return out, nil
}
