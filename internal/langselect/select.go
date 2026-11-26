package langselect

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"rolerelay/internal/domain"
)

var (
	bothRequestPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(both\s+english\s+and\s+spanish|english\s+and\s+spanish\s+(cv|resume|version|versions)|cv\s+in\s+english\s+and\s+spanish|resume\s+in\s+english\s+and\s+spanish)\b`),
		regexp.MustCompile(`(?i)\b(cv\s+en\s+ingl[eé]s\s+y\s+espa[nñ]ol|curr[ií]culum\s+en\s+ingl[eé]s\s+y\s+espa[nñ]ol|en\s+espa[nñ]ol\s+e\s+ingl[eé]s|en\s+ingl[eé]s\s+y\s+espa[nñ]ol|ambos\s+idiomas|both\s+languages)\b`),
	}
	spanishRequestPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b((cv|resume|apply|application|submit)\s+(must\s+be\s+)?in\s+spanish|spanish\s+(cv|resume)\s+(only|required|preferred))\b`),
		regexp.MustCompile(`(?i)\b((enviar|adjuntar|presentar)\s+(tu\s+|su\s+)?(cv|curr[ií]culum|hoja\s+de\s+vida)\s+en\s+espa[nñ]ol|(cv|curr[ií]culum)\s+en\s+espa[nñ]ol|(aplicar|postularse)\s+en\s+espa[nñ]ol)\b`),
	}
	englishRequestPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b((cv|resume|apply|application|submit)\s+(must\s+be\s+)?in\s+english|english\s+(cv|resume)\s+(only|required|preferred)|please\s+submit\s+your\s+(cv|resume)\s+in\s+english)\b`),
		regexp.MustCompile(`(?i)\b((enviar|adjuntar|presentar)\s+(tu\s+|su\s+)?(cv|curr[ií]culum|hoja\s+de\s+vida)\s+en\s+ingl[eé]s|(cv|curr[ií]culum)\s+en\s+ingl[eé]s|(aplicar|postularse)\s+en\s+ingl[eé]s)\b`),
	}

	englishWords = map[string]int{
		"the": 2, "and": 2, "with": 2, "for": 1, "you": 2, "your": 2, "our": 2, "we": 2,
		"are": 2, "will": 2, "have": 2, "from": 1, "that": 1, "this": 1, "requirements": 3,
		"required": 3, "experience": 3, "years": 2, "engineer": 2, "developer": 2, "skills": 3,
		"responsibilities": 3, "preferred": 3, "nice": 1, "looking": 2, "team": 2, "work": 2,
		"must": 3, "strong": 2, "knowledge": 2, "building": 2, "salary": 2, "benefits": 2,
		"remote": 1, "role": 2, "position": 2, "apply": 2, "resume": 2, "about": 2, "what": 2,
	}

	spanishWords = map[string]int{
		"el": 2, "la": 2, "los": 2, "las": 2, "del": 2, "con": 2, "para": 2, "por": 2,
		"una": 2, "como": 2, "sobre": 2, "requisitos": 4, "requerido": 3, "requeridos": 3,
		"indispensable": 4, "excluyente": 4, "experiencia": 4, "años": 4, "anos": 2,
		"ingeniero": 3, "ingeniera": 3, "desarrollador": 3, "desarrolladora": 3, "equipo": 3,
		"buscamos": 4, "estamos": 3, "buscando": 3, "trabajo": 3, "puesto": 3, "empresa": 3,
		"conocimientos": 3, "habilidades": 3, "deseable": 4, "beneficios": 3, "remoto": 2,
		"sueldo": 3, "salario": 3, "remuneración": 4, "remuneracion": 3, "postularse": 3,
		"aplicar": 2, "responsabilidades": 4, "nuestro": 3, "nuestra": 3, "debe": 2, "tener": 2,
	}
)

func DetectRequestedLanguage(text string) string {
	for _, re := range bothRequestPatterns {
		if re.MatchString(text) {
			return "both"
		}
	}
	hasES := false
	for _, re := range spanishRequestPatterns {
		if re.MatchString(text) {
			hasES = true
			break
		}
	}
	hasEN := false
	for _, re := range englishRequestPatterns {
		if re.MatchString(text) {
			hasEN = true
			break
		}
	}
	if hasES && hasEN {
		return "both"
	}
	if hasES {
		return "es"
	}
	if hasEN {
		return "en"
	}
	return ""
}

func DetectListingLanguage(title, description string) string {
	combined := title + "\n" + description
	tokens := tokenizeWords(combined)
	enScore := 0
	esScore := 0

	for _, tok := range tokens {
		if w, ok := englishWords[tok]; ok {
			enScore += w
		}
		if w, ok := spanishWords[tok]; ok {
			esScore += w
		}
	}

	// Spanish diacritics/punctuation add modest signal.
	for _, r := range combined {
		switch r {
		case 'á', 'é', 'í', 'ó', 'ú', 'ñ', 'ü', '¿', '¡', 'Á', 'É', 'Í', 'Ó', 'Ú', 'Ñ':
			esScore++
		}
	}

	total := enScore + esScore
	if total < 4 {
		return "ambiguous"
	}
	if enScore >= esScore*2 && enScore >= 4 {
		return "en"
	}
	if esScore >= enScore*2 && esScore >= 4 {
		return "es"
	}
	// Margin check for longer texts (e.g. Spanish job post with English tech terms).
	enRatio := float64(enScore) / float64(total)
	esRatio := float64(esScore) / float64(total)
	if enRatio >= 0.65 {
		return "en"
	}
	if esRatio >= 0.65 {
		return "es"
	}
	return "ambiguous"
}

type SelectionResult struct {
	Status          domain.LanguageStatus
	TargetLanguage  string // "en", "es", "both", "ambiguous"
	SelectedCVs     []domain.CVDocument
	Roles           []string // "primary", "secondary" matching SelectedCVs
	NeedsReview     bool
	IsMismatch      bool
	Explanation     string
	MissingAnswers  []domain.MissingAnswer
}

func SelectCVs(
	profile domain.Profile,
	cvs []domain.CVDocument,
	vacancy domain.Vacancy,
	channelSupportsMultiple bool,
	overrideLanguage domain.CVLanguage,
) SelectionResult {
	activeByLang := make(map[domain.CVLanguage]domain.CVDocument)
	for _, doc := range cvs {
		if doc.IsActive {
			activeByLang[doc.Language] = doc
		}
	}

	// Enforce language mode CV prerequisites first (Both mode requires BOTH EN and ES CVs).
	for _, reqLang := range profile.LanguageMode.RequiredCVLanguages() {
		if _, ok := activeByLang[reqLang]; !ok {
			langLabel := "English"
			if reqLang == domain.CVLanguageES {
				langLabel = "Spanish"
			}
			msg := fmt.Sprintf("Profile language mode '%s' requires an active %s PDF CV before evaluating vacancies.", profile.LanguageMode, langLabel)
			if profile.LanguageMode == domain.LanguageModeBoth {
				msg = fmt.Sprintf("Both mode requires two separate PDF CVs (English and Spanish). Upload your %s PDF CV to continue.", langLabel)
			}
			return SelectionResult{
				Status:      domain.LanguageStatusMissingCV,
				NeedsReview: true,
				Explanation: msg,
			}
		}
	}

	// Determine target language: explicit override > vacancy requested > listing detected > ambiguous.
	target := strings.ToLower(strings.TrimSpace(vacancy.RequestedLanguage))
	if overrideLanguage.Valid() {
		target = string(overrideLanguage)
	}
	if target == "" {
		detected := strings.ToLower(strings.TrimSpace(vacancy.DetectedLanguage))
		if detected == "en" || detected == "es" {
			target = detected
		} else {
			target = "ambiguous"
		}
	}

	switch target {
	case "ambiguous":
		options := make([]string, 0, 2)
		for _, lang := range profile.LanguageMode.RequiredCVLanguages() {
			options = append(options, string(lang))
		}
		return SelectionResult{
			Status:         domain.LanguageStatusAmbiguous,
			TargetLanguage: "ambiguous",
			NeedsReview:    true,
			Explanation:    "The vacancy does not specify a CV language and the listing language is ambiguous. Choose which CV language to use in the review panel.",
			MissingAnswers: []domain.MissingAnswer{
				{
					Key:      "cv_language",
					Label:    "CV language for this application",
					Kind:     "cv_language",
					Required: true,
					Options:  options,
					Reason:   "Vacancy language is ambiguous; select English or Spanish CV.",
				},
			},
		}

	case "both":
		if profile.LanguageMode != domain.LanguageModeBoth {
			return SelectionResult{
				Status:         domain.LanguageStatusMismatch,
				TargetLanguage: "both",
				IsMismatch:     true,
				Explanation:    fmt.Sprintf("Vacancy explicitly requests CVs in both English and Spanish, but your profile is set to '%s' mode.", profile.LanguageMode),
			}
		}
		enDoc := activeByLang[domain.CVLanguageEN]
		esDoc := activeByLang[domain.CVLanguageES]
		primary, secondary := enDoc, esDoc
		if vacancy.DetectedLanguage == "es" {
			primary, secondary = esDoc, enDoc
		}
		if channelSupportsMultiple {
			return SelectionResult{
				Status:         domain.LanguageStatusOK,
				TargetLanguage: "both",
				SelectedCVs:    []domain.CVDocument{primary, secondary},
				Roles:          []string{"primary", "secondary"},
				Explanation:    fmt.Sprintf("Vacancy requested both languages and the channel supports multiple attachments; attaching %s v%d and %s v%d.", strings.ToUpper(string(primary.Language)), primary.Version, strings.ToUpper(string(secondary.Language)), secondary.Version),
			}
		}
		return SelectionResult{
			Status:         domain.LanguageStatusChannelNote,
			TargetLanguage: "both",
			SelectedCVs:    []domain.CVDocument{primary},
			Roles:          []string{"primary"},
			NeedsReview:    true,
			Explanation:    fmt.Sprintf("Vacancy requested both English and Spanish CVs, but this submission channel only supports a single attachment. Selected %s v%d as primary; attach both manually if using the external form.", strings.ToUpper(string(primary.Language)), primary.Version),
		}

	case "en", "es":
		cvLang := domain.CVLanguage(target)
		if profile.LanguageMode != domain.LanguageModeBoth && string(profile.LanguageMode) != target {
			return SelectionResult{
				Status:         domain.LanguageStatusMismatch,
				TargetLanguage: target,
				IsMismatch:     true,
				Explanation:    fmt.Sprintf("Language mismatch: vacancy requires %s, which is outside your configured '%s' mode.", langDisplayName(cvLang), profile.LanguageMode),
			}
		}
		doc := activeByLang[cvLang]
		return SelectionResult{
			Status:         domain.LanguageStatusOK,
			TargetLanguage: target,
			SelectedCVs:    []domain.CVDocument{doc},
			Roles:          []string{"primary"},
			Explanation:    fmt.Sprintf("Selected %s CV (%s, v%d) based on vacancy language.", langDisplayName(cvLang), doc.Filename, doc.Version),
		}

	default:
		return SelectionResult{
			Status:         domain.LanguageStatusAmbiguous,
			TargetLanguage: "ambiguous",
			NeedsReview:    true,
			Explanation:    "Could not determine CV language for this vacancy.",
		}
	}
}

func langDisplayName(lang domain.CVLanguage) string {
	if lang == domain.CVLanguageES {
		return "Spanish"
	}
	return "English"
}

func tokenizeWords(text string) []string {
	var out []string
	var cur strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) {
			cur.WriteRune(r)
		} else if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
