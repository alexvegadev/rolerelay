package evaluator

import (
	"strings"
	"unicode"

	"rolerelay/internal/domain"
)

func VerifyFindings(findings []domain.RequirementFinding, cvs []domain.CVDocument) []domain.RequirementFinding {
	normalizedByLang := make(map[domain.CVLanguage]string, len(cvs))
	normalizedAll := make([]string, 0, len(cvs))
	for _, doc := range cvs {
		norm := normalizeForMatch(doc.ExtractedText)
		normalizedByLang[doc.Language] = norm
		normalizedAll = append(normalizedAll, norm)
	}

	verified := make([]domain.RequirementFinding, 0, len(findings))
	for _, f := range findings {
		if f.Kind != domain.RequirementPreference {
			f.Kind = domain.RequirementMandatory
		}
		excerpt := strings.TrimSpace(f.EvidenceExcerpt)
		normExcerpt := normalizeForMatch(excerpt)

		found := false
		if countAlphaNum(normExcerpt) >= 4 {
			if f.CVLanguage.Valid() {
				if cvText, ok := normalizedByLang[f.CVLanguage]; ok && strings.Contains(cvText, normExcerpt) {
					found = true
				}
			}
			if !found {
				for lang, cvText := range normalizedByLang {
					if strings.Contains(cvText, normExcerpt) {
						found = true
						if !f.CVLanguage.Valid() {
							f.CVLanguage = lang
						}
						break
					}
				}
			}
		}

		f.EvidenceVerified = found
		if f.Met && !found {
			f.Met = false
			if excerpt == "" {
				f.Reason = appendReason(f.Reason, "No supporting CV excerpt was cited; Go verification requires evidence from the selected CV.")
			} else {
				f.Reason = appendReason(f.Reason, "Cited excerpt could not be verified in the selected CV text.")
			}
		}
		verified = append(verified, f)
	}
	return verified
}

func normalizeForMatch(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'“”‘’`)
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		lastSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func countAlphaNum(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

func appendReason(existing, note string) string {
	existing = strings.TrimSpace(existing)
	if existing == "" {
		return note
	}
	if strings.Contains(existing, note) {
		return existing
	}
	return existing + " " + note
}
