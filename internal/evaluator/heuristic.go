package evaluator

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"rolerelay/internal/domain"
)

type LocalHeuristicEvaluator struct{}

func NewLocalHeuristicEvaluator() *LocalHeuristicEvaluator {
	return &LocalHeuristicEvaluator{}
}

func (e *LocalHeuristicEvaluator) Name() string {
	return "local-bilingual-heuristic-v1"
}

var (
	salaryPromptRe = regexp.MustCompile(`(?i)\b(salary\s+expectation|expected\s+salary|desired\s+salary|compensation\s+expectation|pretensi[oó]n\s+salarial|remuneraci[oó]n\s+pretendida|expectativa\s+salarial|rango\s+salarial\s+esperado)\b`)
	bulletPrefixRe = regexp.MustCompile(`^\s*(?:[-*•–—]|\d+[.)])\s+`)
	stopWords      = map[string]bool{
		"the": true, "and": true, "with": true, "for": true, "from": true, "that": true,
		"this": true, "have": true, "has": true, "are": true, "will": true, "your": true,
		"our": true, "you": true, "years": true, "year": true, "experience": true, "strong": true,
		"solid": true, "good": true, "proven": true, "ability": true, "skills": true, "skill": true,
		"working": true, "work": true, "knowledge": true, "understanding": true, "using": true,
		"building": true, "production": true, "systems": true, "applications": true, "required": true,
		"mandatory": true, "must": true, "preferred": true, "nice": true, "plus": true, "bonus": true,
		"familiarity": true, "proficiency": true, "minimum": true, "least": true,
		"con": true, "para": true, "por": true, "una": true, "uno": true, "los": true, "las": true,
		"del": true, "que": true, "como": true, "sobre": true, "experiencia": true, "años": true,
		"anos": true, "conocimientos": true, "habilidades": true, "manejo": true, "dominio": true,
		"trabajo": true, "equipo": true, "desarrollo": true, "sistemas": true, "aplicaciones": true,
		"requerido": true, "requisitos": true, "excluyente": true, "indispensable": true,
		"deseable": true, "valorable": true, "valoramos": true, "buen": true, "nivel": true,
		"tener": true, "contar": true, "mínimo": true, "minimo": true, "al": true, "en": true, "de": true,
	}
)

type parsedRequirement struct {
	text string
	kind domain.RequirementKind
}

func (e *LocalHeuristicEvaluator) Evaluate(_ context.Context, input ModelInput) (ModelOutput, error) {
	reqs := extractRequirements(input.Vacancy.Title, input.Vacancy.Description)
	if len(reqs) == 0 {
		reqs = []parsedRequirement{
			{
				text: strings.TrimSpace(input.Vacancy.Title),
				kind: domain.RequirementMandatory,
			},
		}
	}

	findings := make([]domain.RequirementFinding, 0, len(reqs))
	mandatoryTotal := 0
	mandatoryMet := 0
	prefTotal := 0
	prefMet := 0

	for _, req := range reqs {
		if req.kind == domain.RequirementMandatory {
			mandatoryTotal++
		} else {
			prefTotal++
		}

		excerpt, matchedLang, matchedTerms, missingTerms := findBestCVEvidence(req.text, input.CVs)
		met := excerpt != "" && len(missingTerms) == 0
		if !met && excerpt != "" && len(matchedTerms) > 0 && float64(len(matchedTerms))/float64(len(matchedTerms)+len(missingTerms)) >= 0.6 {
			met = true
		}

		var reason string
		if met {
			if req.kind == domain.RequirementMandatory {
				mandatoryMet++
			} else {
				prefMet++
			}
			reason = fmt.Sprintf("Matched in %s CV (%s).", strings.ToUpper(string(matchedLang)), strings.Join(matchedTerms, ", "))
		} else if len(missingTerms) > 0 {
			reason = fmt.Sprintf("Insufficient CV evidence for: %s.", strings.Join(missingTerms, ", "))
			excerpt = ""
			matchedLang = ""
		} else {
			reason = "No matching experience found in the selected CV."
			excerpt = ""
			matchedLang = ""
		}

		findings = append(findings, domain.RequirementFinding{
			Requirement:      req.text,
			Kind:             req.kind,
			Met:              met,
			EvidenceExcerpt:  excerpt,
			CVLanguage:       matchedLang,
			EvidenceVerified: false, // Verified downstream by Go VerifyFindings
			Reason:           reason,
		})
	}

	// Score weighted 80% mandatory, 20% preferences (or 100% mandatory if no preferences).
	var fitScore int
	if mandatoryTotal > 0 && prefTotal > 0 {
		mScore := (float64(mandatoryMet) / float64(mandatoryTotal)) * 80.0
		pScore := (float64(prefMet) / float64(prefTotal)) * 20.0
		fitScore = int(mScore + pScore + 0.5)
	} else if mandatoryTotal > 0 {
		fitScore = int((float64(mandatoryMet)/float64(mandatoryTotal))*100.0 + 0.5)
	} else if prefTotal > 0 {
		fitScore = int((float64(prefMet)/float64(prefTotal))*100.0 + 0.5)
	}

	missingAnswers := make([]domain.MissingAnswer, 0)
	if salaryPromptRe.MatchString(input.Vacancy.Description) {
		if _, answered := input.Answers["salary_expectation"]; !answered {
			missingAnswers = append(missingAnswers, domain.MissingAnswer{
				Key:      "salary_expectation",
				Label:    "Expected salary",
				Kind:     "salary",
				Required: true,
				Reason:   "The vacancy asks for a salary expectation, which is not yet recorded for this application.",
			})
		}
	}

	summary := fmt.Sprintf(
		"Matched %d/%d mandatory requirements and %d/%d preferences (estimated fit %d%%).",
		mandatoryMet, mandatoryTotal, prefMet, prefTotal, fitScore,
	)

	return ModelOutput{
		ModelName:      e.Name(),
		FitScore:       fitScore,
		Summary:        summary,
		Findings:       findings,
		MissingAnswers: missingAnswers,
	}, nil
}

func extractRequirements(title, description string) []parsedRequirement {
	lines := strings.Split(strings.ReplaceAll(description, "\r\n", "\n"), "\n")
	sectionKind := domain.RequirementMandatory
	inReqSection := false

	var out []parsedRequirement
	seen := make(map[string]bool)

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)

		if isPreferenceHeader(lower) {
			sectionKind = domain.RequirementPreference
			inReqSection = true
			continue
		}
		if isMandatoryHeader(lower) {
			sectionKind = domain.RequirementMandatory
			inReqSection = true
			continue
		}
		if isNonRequirementHeader(lower) {
			inReqSection = false
			continue
		}

		isBullet := bulletPrefixRe.MatchString(rawLine)
		cleaned := strings.TrimSpace(bulletPrefixRe.ReplaceAllString(line, ""))
		if cleaned == "" || len(cleaned) < 5 {
			continue
		}
		// Skip pure email / boilerplate instruction lines from requirements.
		if strings.Contains(lower, "@") && (strings.Contains(lower, "send") || strings.Contains(lower, "enviar") || strings.Contains(lower, "apply")) {
			continue
		}
		if salaryPromptRe.MatchString(cleaned) && len(extractKeywords(cleaned)) <= 2 {
			continue
		}

		kind := sectionKind
		if strings.Contains(lower, "nice to have") || strings.Contains(lower, "preferred") || strings.Contains(lower, "deseable") || strings.Contains(lower, "opcional") || strings.Contains(lower, "plus:") {
			kind = domain.RequirementPreference
		} else if strings.Contains(lower, "must have") || strings.Contains(lower, "required") || strings.Contains(lower, "mandatory") || strings.Contains(lower, "excluyente") || strings.Contains(lower, "indispensable") {
			kind = domain.RequirementMandatory
		}

		if isBullet || inReqSection || hasExplicitRequirementCue(lower) {
			if len(extractKeywords(cleaned)) == 0 {
				continue
			}
			key := strings.ToLower(cleaned)
			if !seen[key] {
				seen[key] = true
				out = append(out, parsedRequirement{text: cleaned, kind: kind})
			}
		}
	}

	if len(out) == 0 && strings.TrimSpace(title) != "" {
		out = append(out, parsedRequirement{text: strings.TrimSpace(title), kind: domain.RequirementMandatory})
	}
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

func isMandatoryHeader(lower string) bool {
	if len(lower) > 65 {
		return false
	}
	cues := []string{
		"requirements", "required", "must have", "must-have", "qualifications",
		"what you bring", "what we're looking for", "what we look for",
		"requisitos", "requisitos excluyentes", "indispensable", "lo que buscamos", "perfil buscado",
	}
	for _, c := range cues {
		if strings.Contains(lower, c) && !isPreferenceHeader(lower) {
			return true
		}
	}
	return false
}

func isPreferenceHeader(lower string) bool {
	if len(lower) > 65 {
		return false
	}
	cues := []string{
		"nice to have", "nice-to-have", "preferred", "bonus", "plus",
		"deseable", "deseables", "opcional", "valoramos", "será un plus", "sera un plus",
	}
	for _, c := range cues {
		if strings.Contains(lower, c) {
			return true
		}
	}
	return false
}

func isNonRequirementHeader(lower string) bool {
	if len(lower) > 60 {
		return false
	}
	cues := []string{
		"benefits", "perks", "about us", "about the company", "how to apply",
		"beneficios", "sobre nosotros", "cómo aplicar", "como aplicar", "ofrecemos",
	}
	for _, c := range cues {
		if strings.HasPrefix(lower, c) || strings.HasSuffix(lower, c+":") {
			return true
		}
	}
	return false
}

func hasExplicitRequirementCue(lower string) bool {
	cues := []string{"must have", "required:", "mandatory:", "excluyente", "indispensable", "deseable:", "preferred:"}
	for _, c := range cues {
		if strings.Contains(lower, c) {
			return true
		}
	}
	return false
}

func findBestCVEvidence(requirement string, cvs []domain.CVDocument) (excerpt string, lang domain.CVLanguage, matched []string, missing []string) {
	keywords := extractKeywords(requirement)
	if len(keywords) == 0 {
		return "", "", nil, nil
	}

	bestScore := 0
	var bestExcerpt string
	var bestLang domain.CVLanguage
	var bestMatched []string

	for _, doc := range cvs {
		cvLower := strings.ToLower(doc.ExtractedText)
		docMatched := make([]string, 0, len(keywords))
		for _, kw := range keywords {
			if containsTerm(cvLower, kw) {
				docMatched = append(docMatched, kw)
			}
		}
		if len(docMatched) == 0 {
			continue
		}

		// Pick the single line/sentence from the CV with the highest keyword overlap as verbatim excerpt.
		segments := splitCVSegments(doc.ExtractedText)
		segBestText := ""
		segBestHits := 0
		for _, seg := range segments {
			segLower := strings.ToLower(seg)
			hits := 0
			for _, kw := range docMatched {
				if containsTerm(segLower, kw) {
					hits++
				}
			}
			if hits > segBestHits {
				segBestHits = hits
				segBestText = strings.TrimSpace(seg)
			}
		}
		if segBestText == "" && len(segments) > 0 {
			segBestText = strings.TrimSpace(segments[0])
		}
		if len(segBestText) > 220 {
			segBestText = strings.TrimSpace(segBestText[:220])
		}

		if len(docMatched) > bestScore {
			bestScore = len(docMatched)
			bestExcerpt = segBestText
			bestLang = doc.Language
			bestMatched = docMatched
		}
	}

	matchedSet := make(map[string]bool, len(bestMatched))
	for _, m := range bestMatched {
		matchedSet[m] = true
	}
	for _, kw := range keywords {
		if !matchedSet[kw] {
			missing = append(missing, kw)
		}
	}
	return bestExcerpt, bestLang, bestMatched, missing
}

func splitCVSegments(text string) []string {
	rawLines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var segments []string
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) <= 180 {
			segments = append(segments, line)
			continue
		}
		// Break long lines on period + space while keeping verbatim substrings intact.
		parts := strings.Split(line, ". ")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if len(p) >= 6 {
				segments = append(segments, p)
			}
		}
	}
	return segments
}

func extractKeywords(text string) []string {
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		tok := cur.String()
		cur.Reset()
		if tok == "go" || tok == "c#" || tok == "c++" || tok == "ai" || tok == "ml" || tok == "ci" || tok == "cd" || tok == "ts" || tok == "js" || tok == "qa" {
			tokens = append(tokens, tok)
			return
		}
		if len([]rune(tok)) < 3 || stopWords[tok] {
			return
		}
		if isAllDigits(tok) {
			return
		}
		tokens = append(tokens, tok)
	}

	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' || r == '#' || r == '.' {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	seen := make(map[string]bool)
	unique := make([]string, 0, len(tokens))
	for _, t := range tokens {
		t = strings.Trim(t, ".")
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		unique = append(unique, t)
	}
	return unique
}

func containsTerm(haystackLower, term string) bool {
	if len(term) <= 2 {
		// Word-boundary check for short tokens like "go", "ts", "js", "ai".
		re := regexp.MustCompile(`(?i)(^|[^a-z0-9_])` + regexp.QuoteMeta(term) + `($|[^a-z0-9_])`)
		return re.MatchString(haystackLower)
	}
	return strings.Contains(haystackLower, term)
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 0
}
