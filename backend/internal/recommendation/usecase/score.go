package usecase

import (
	"errors"
	"math"
)

// ScoreMap turns a cosine similarity into the 0 to 100 score a recommendation shows
// (AI-2.2). e5 puts most pairs of profiles between 0.7 and 0.9, so the raw cosine as a
// percentage would make every pair look alike. The map is a logistic curve (Platt
// scaling), 100 / (1 + exp(-(Slope*cos + Intercept))), fitted by `make calibrate-recs` on
// the sample TUNING split only: it estimates how likely two profiles at that similarity
// share their main research area (docs/evaluation.md).
//
// The constants belong to the embedding model they were fitted for (Model, "name@revision").
// Vectors of another model are spread differently, so a model change needs a refit; the
// service logs a warning while they differ.
type ScoreMap struct {
	Model     string
	Slope     float64
	Intercept float64
}

// Validate refuses a map that would not keep the order of similarity: Slope must be
// positive, so a higher cosine never gets a lower score.
func (m ScoreMap) Validate() error {
	if m.Model == "" {
		return errors.New("score map: model is empty")
	}
	if !(m.Slope > 0) || math.IsInf(m.Slope, 0) || math.IsNaN(m.Intercept) || math.IsInf(m.Intercept, 0) {
		return errors.New("score map: slope must be a positive number and intercept a finite number")
	}
	return nil
}

// Score maps cos (from -1 to 1) to a whole number from 0 to 100. It never decreases as
// cos grows.
func (m ScoreMap) Score(cos float64) int {
	if math.IsNaN(cos) {
		return 0
	}
	p := 1 / (1 + math.Exp(-(m.Slope*cos + m.Intercept)))
	s := int(math.Round(100 * p))
	return min(max(s, 0), 100)
}
