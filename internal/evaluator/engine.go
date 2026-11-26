package evaluator

import (
	"context"
	"fmt"
	"strings"

	"rolerelay/internal/domain"
	"rolerelay/internal/langselect"
	"rolerelay/internal/store"
)

func EvaluateVacancy(
	ctx context.Context,
	profile domain.Profile,
	cvs []domain.CVDocument,
	vacancy domain.Vacancy,
	existingAnswers map[string]any,
	modelEval ModelEvaluator,
) (store.SaveEvaluationParams, error) {
	if modelEval == nil {
		modelEval = NewLocalHeuristicEvaluator()
	}

	channel := "manual"
	recipient := ""
	externalURL := vacancy.ApplicationURL
	if strings.TrimSpace(vacancy.ApplicationEmail) != "" {
		channel = "email"
		recipient = strings.TrimSpace(vacancy.ApplicationEmail)
	} else if strings.TrimSpace(vacancy.ApplicationURL) != "" {
		channel = "external_form"
	}
	channelSupportsMultiple := channel == "email"

	var overrideLang domain.CVLanguage
	if existingAnswers != nil {
		if raw, ok := existingAnswers["cv_language"].(string); ok {
			candidate := domain.CVLanguage(strings.ToLower(strings.TrimSpace(raw)))
			if candidate.Valid() {
				overrideLang = candidate
			}
		}
	}

	selection := langselect.SelectCVs(profile, cvs, vacancy, channelSupportsMultiple, overrideLang)

	selectedIDs := make([]int64, 0, len(selection.SelectedCVs))
	for _, doc := range selection.SelectedCVs {
		selectedIDs = append(selectedIDs, doc.ID)
	}

	// Early return if missing required CV, ambiguous language, or language mismatch.
	switch selection.Status {
	case domain.LanguageStatusMissingCV:
		return store.SaveEvaluationParams{
			Evaluation: domain.Evaluation{
				VacancyID:       vacancy.ID,
				ProfileID:       profile.ID,
				SelectedCVIDs:   []int64{},
				ModelName:       "rules:cv-selection",
				FitScore:        0,
				PassedFilters:   false,
				PassedThreshold: false,
				LanguageStatus:  selection.Status,
				Summary:         selection.Explanation,
				Findings:        []domain.RequirementFinding{},
				MissingAnswers:  []domain.MissingAnswer{},
			},
			ApplicationState: domain.StatePending,
			Channel:          channel,
			Recipient:        recipient,
			ExternalApplyURL: externalURL,
			PendingReason:    selection.Explanation,
			StateReason:      selection.Explanation,
			StateSource:      "system",
		}, nil

	case domain.LanguageStatusAmbiguous:
		return store.SaveEvaluationParams{
			Evaluation: domain.Evaluation{
				VacancyID:       vacancy.ID,
				ProfileID:       profile.ID,
				SelectedCVIDs:   []int64{},
				ModelName:       "rules:cv-selection",
				FitScore:        0,
				PassedFilters:   true,
				PassedThreshold: false,
				LanguageStatus:  selection.Status,
				Summary:         selection.Explanation,
				Findings:        []domain.RequirementFinding{},
				MissingAnswers:  selection.MissingAnswers,
			},
			ApplicationState: domain.StatePending,
			Channel:          channel,
			Recipient:        recipient,
			ExternalApplyURL: externalURL,
			PendingReason:    selection.Explanation,
			StateReason:      selection.Explanation,
			StateSource:      "system",
		}, nil

	case domain.LanguageStatusMismatch:
		return store.SaveEvaluationParams{
			Evaluation: domain.Evaluation{
				VacancyID:       vacancy.ID,
				ProfileID:       profile.ID,
				SelectedCVIDs:   []int64{},
				ModelName:       "rules:cv-selection",
				FitScore:        0,
				PassedFilters:   false,
				PassedThreshold: false,
				LanguageStatus:  selection.Status,
				Summary:         selection.Explanation,
				Findings:        []domain.RequirementFinding{},
				MissingAnswers:  []domain.MissingAnswer{},
			},
			ApplicationState: domain.StateDiscarded,
			Channel:          channel,
			Recipient:        recipient,
			ExternalApplyURL: externalURL,
			DiscardReason:    selection.Explanation,
			StateReason:      selection.Explanation,
			StateSource:      "system",
		}, nil
	}

	// Check configured work-arrangement filter.
	passedFilters := true
	filterMismatchReason := ""
	if profile.WorkArrangement != domain.WorkArrangementAny &&
		vacancy.WorkArrangement != "" &&
		vacancy.WorkArrangement != domain.WorkArrangementUnknown &&
		vacancy.WorkArrangement != profile.WorkArrangement {
		passedFilters = false
		filterMismatchReason = fmt.Sprintf(
			"Work arrangement mismatch: vacancy is '%s', while your profile requires '%s'.",
			vacancy.WorkArrangement,
			profile.WorkArrangement,
		)
	}

	modelOut, err := modelEval.Evaluate(ctx, ModelInput{
		Profile: profile,
		Vacancy: vacancy,
		CVs:     selection.SelectedCVs,
		Answers: existingAnswers,
	})
	if err != nil {
		return store.SaveEvaluationParams{}, fmt.Errorf("evaluate vacancy %d with %s: %w", vacancy.ID, modelEval.Name(), err)
	}

	verifiedFindings := VerifyFindings(modelOut.Findings, selection.SelectedCVs)

	// Recompute unmet mandatory requirements strictly using Go-verified evidence.
	var unmetMandatory []string
	mandatoryTotal := 0
	mandatoryVerified := 0
	for _, f := range verifiedFindings {
		if f.Kind == domain.RequirementMandatory {
			mandatoryTotal++
			if f.Met && f.EvidenceVerified {
				mandatoryVerified++
			} else {
				unmetMandatory = append(unmetMandatory, f.Requirement)
			}
		}
	}

	fitScore := modelOut.FitScore
	if mandatoryTotal > 0 && len(unmetMandatory) > 0 {
		// Ensure hallucinated/unverified mandatory claims cannot inflate fitScore above verified mandatory share.
		verifiedCap := int((float64(mandatoryVerified)/float64(mandatoryTotal))*85.0 + 15.0)
		if fitScore > verifiedCap {
			fitScore = verifiedCap
		}
	}

	passedThreshold := fitScore >= profile.FitThreshold
	missingAnswers := modelOut.MissingAnswers
	if missingAnswers == nil {
		missingAnswers = []domain.MissingAnswer{}
	}

	summary := strings.TrimSpace(modelOut.Summary)
	if selection.Explanation != "" {
		if summary == "" {
			summary = selection.Explanation
		} else {
			summary = selection.Explanation + " " + summary
		}
	}

	params := store.SaveEvaluationParams{
		Evaluation: domain.Evaluation{
			VacancyID:       vacancy.ID,
			ProfileID:       profile.ID,
			SelectedCVIDs:   selectedIDs,
			ModelName:       modelOut.ModelName,
			FitScore:        fitScore,
			PassedFilters:   passedFilters,
			PassedThreshold: passedThreshold,
			LanguageStatus:  selection.Status,
			Summary:         summary,
			Findings:        verifiedFindings,
			MissingAnswers:  missingAnswers,
		},
		SelectedCVRoles:  selection.Roles,
		Channel:          channel,
		Recipient:        recipient,
		ExternalApplyURL: externalURL,
		StateSource:      "system",
	}

	switch {
	case !passedFilters:
		params.ApplicationState = domain.StateDiscarded
		params.DiscardReason = filterMismatchReason
		params.StateReason = filterMismatchReason

	case len(unmetMandatory) > 0:
		reason := fmt.Sprintf(
			"Pending review: insufficient verified CV evidence for %d mandatory requirement(s): %s.",
			len(unmetMandatory),
			strings.Join(unmetMandatory, "; "),
		)
		params.ApplicationState = domain.StatePending
		params.PendingReason = reason
		params.StateReason = reason

	case len(missingAnswers) > 0:
		labels := make([]string, 0, len(missingAnswers))
		for _, m := range missingAnswers {
			labels = append(labels, m.Label)
		}
		reason := fmt.Sprintf("Pending review: missing required answer(s): %s.", strings.Join(labels, ", "))
		params.ApplicationState = domain.StatePending
		params.PendingReason = reason
		params.StateReason = reason

	case selection.NeedsReview:
		params.ApplicationState = domain.StatePending
		params.PendingReason = selection.Explanation
		params.StateReason = selection.Explanation

	case !passedThreshold:
		reason := fmt.Sprintf("Fit score %d%% is below your configured minimum threshold of %d%%.", fitScore, profile.FitThreshold)
		params.ApplicationState = domain.StateDiscarded
		params.DiscardReason = reason
		params.StateReason = reason

	default:
		params.ApplicationState = domain.StatePending
		if channel == "email" {
			params.Subject = buildApplicationSubject(vacancy, selection.TargetLanguage)
			params.Body = buildApplicationBody(profile, vacancy, selection.TargetLanguage, existingAnswers)
			if profile.AutoSendEnabled {
				params.QueuedForSend = true
				params.PendingReason = fmt.Sprintf("Qualified (%d%% fit). Queued for automatic email submission to %s.", fitScore, recipient)
			} else {
				params.PendingReason = fmt.Sprintf("Qualified (%d%% fit). Ready for email submission to %s (enable auto-send or queue manually).", fitScore, recipient)
			}
			params.StateReason = params.PendingReason
		} else {
			params.PendingReason = fmt.Sprintf("Qualified (%d%% fit). Manual submission required via the official application form.", fitScore)
			params.StateReason = params.PendingReason
		}
	}

	return params, nil
}

func buildApplicationSubject(v domain.Vacancy, targetLang string) string {
	if targetLang == "es" {
		return fmt.Sprintf("Postulación: %s", v.Title)
	}
	return fmt.Sprintf("Application: %s", v.Title)
}

func buildApplicationBody(p domain.Profile, v domain.Vacancy, targetLang string, answers map[string]any) string {
	name := strings.TrimSpace(p.FullName)
	if name == "" {
		name = "Candidate"
	}
	extra := ""
	if answers != nil {
		if sal, ok := answers["salary_expectation"].(map[string]any); ok {
			amt := sal["amount"]
			cur := sal["currency"]
			if amt != nil && cur != nil {
				if targetLang == "es" {
					extra = fmt.Sprintf("\n\nPretensión salarial: %v %v", cur, amt)
				} else {
					extra = fmt.Sprintf("\n\nSalary expectation: %v %v", cur, amt)
				}
			}
		}
	}
	if targetLang == "es" {
		return fmt.Sprintf("Hola equipo de %s,\n\nAdjunto mi CV para la posición de %s.%s\n\nSaludos,\n%s", v.CompanyName, v.Title, extra, name)
	}
	return fmt.Sprintf("Hello %s team,\n\nPlease find my CV attached for the %s position.%s\n\nBest regards,\n%s", v.CompanyName, v.Title, extra, name)
}
