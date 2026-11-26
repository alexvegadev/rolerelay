package evaluator

import (
	"context"

	"rolerelay/internal/domain"
)

type ModelInput struct {
	Profile domain.Profile
	Vacancy domain.Vacancy
	CVs     []domain.CVDocument
	Answers map[string]any
}

type ModelOutput struct {
	ModelName      string
	FitScore       int
	Summary        string
	Findings       []domain.RequirementFinding
	MissingAnswers []domain.MissingAnswer
}

type ModelEvaluator interface {
	Name() string
	Evaluate(ctx context.Context, input ModelInput) (ModelOutput, error)
}
