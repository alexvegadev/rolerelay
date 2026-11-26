package connectors

import (
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"rolerelay/internal/domain"
)

var (
	htmlTagBreakRe = regexp.MustCompile(`(?i)</?(p|div|br|li|h[1-6]|tr|ul|ol|section|article)[^>]*>`)
	htmlTagRe      = regexp.MustCompile(`<[^>]+>`)
	emailRe        = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	trackingParams = map[string]bool{
		"utm_source":   true,
		"utm_medium":   true,
		"utm_campaign": true,
		"utm_term":     true,
		"utm_content":  true,
		"gh_src":       true,
		"gh_jid":       false,
		"lever-source": true,
		"lever-origin": true,
		"ref":          true,
		"source":       true,
		"src":          true,
		"fbclid":       true,
		"gclid":        true,
	}
)

type ImportedVacancy struct {
	SourceType       domain.JobBoardProvider
	Provider         string
	CompanyBoard     string
	ExternalJobID    string
	CanonicalURL     string
	CompanyName      string
	Title            string
	Location         string
	WorkArrangement  domain.WorkArrangement
	Description      string
	ApplicationEmail string
	ApplicationURL   string
	RawJSON          string
}

func CanonicalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""

	if u.Path != "/" {
		u.Path = strings.TrimRight(u.Path, "/")
	}

	q := u.Query()
	cleanQ := url.Values{}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lowerK := strings.ToLower(k)
		if trackingParams[lowerK] || strings.HasPrefix(lowerK, "utm_") {
			continue
		}
		vals := q[k]
		for _, v := range vals {
			if strings.TrimSpace(v) != "" {
				cleanQ.Add(lowerK, strings.TrimSpace(v))
			}
		}
	}
	u.RawQuery = cleanQ.Encode()
	return u.String()
}

func HTMLToPlainText(raw string) string {
	if raw == "" {
		return ""
	}
	decoded := html.UnescapeString(raw)
	withBreaks := htmlTagBreakRe.ReplaceAllString(decoded, "\n")
	stripped := htmlTagRe.ReplaceAllString(withBreaks, " ")
	stripped = html.UnescapeString(stripped)
	stripped = strings.ReplaceAll(stripped, "\r\n", "\n")
	stripped = strings.ReplaceAll(stripped, "\r", "\n")

	lines := strings.Split(stripped, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		out = append(out, strings.Join(fields, " "))
	}
	return strings.Join(out, "\n")
}

func ExtractApplicationEmail(text string) string {
	matches := emailRe.FindAllString(text, -1)
	for _, m := range matches {
		lower := strings.ToLower(m)
		if strings.HasPrefix(lower, "noreply@") || strings.HasPrefix(lower, "no-reply@") || strings.HasPrefix(lower, "privacy@") {
			continue
		}
		return lower
	}
	return ""
}

func InferWorkArrangement(location, description string) domain.WorkArrangement {
	combined := strings.ToLower(location + " " + description)
	switch {
	case strings.Contains(combined, "hybrid") || strings.Contains(combined, "híbrido") || strings.Contains(combined, "hibrido"):
		return domain.WorkArrangementHybrid
	case strings.Contains(combined, "remote") || strings.Contains(combined, "remoto") || strings.Contains(combined, "work from home") || strings.Contains(combined, "teletrabajo"):
		return domain.WorkArrangementRemote
	case strings.Contains(combined, "on-site") || strings.Contains(combined, "onsite") || strings.Contains(combined, "in-office") || strings.Contains(combined, "presencial"):
		return domain.WorkArrangementOnsite
	default:
		return domain.WorkArrangementUnknown
	}
}
