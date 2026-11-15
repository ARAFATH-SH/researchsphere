package main

import (
	"context"
	"fmt"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/aiclient"
	profiledomain "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// queryEmbedder gives hybrid researcher search (AI-5.1) its query vectors from the AI
// service: e5's "query: " kind, one text per call. The usecase bounds the wait.
type queryEmbedder struct {
	ai aiclient.Embedder
}

var _ profiledomain.QueryEmbedder = queryEmbedder{}

func (e queryEmbedder) EmbedQuery(ctx context.Context, text string) (profiledomain.QueryVector, error) {
	res, err := e.ai.Embed(ctx, aiclient.KindQuery, []string{text})
	if err != nil {
		return profiledomain.QueryVector{}, fmt.Errorf("embed search query: %w", err)
	}
	if len(res.Vectors) != 1 {
		return profiledomain.QueryVector{}, fmt.Errorf("embed search query: %d vectors, want 1", len(res.Vectors))
	}
	return profiledomain.QueryVector{Model: res.Model, Values: res.Vectors[0]}, nil
}
