// Package usecase builds embedding source text, runs the queue worker and answers the
// administrators' questions about it (AI-1).
package usecase

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/domain"
)

// textVersion is hashed with every text. Changing how text is built or split, without a new
// model, changes it, so every embedding is made again.
const textVersion = "profile-text-v1\n"

// ProfileText is the one place a profile's embedding text is built (AI-1.1): headline,
// interests, skills, bio, then each confirmed publication's title and abstract, newest first.
// Each field's whitespace is collapsed to single spaces and fields are separated by line
// breaks, so the same profile always gives the same text and hash. Headline, interests and
// skills come first, so the AI service's split always puts them in the first chunk. Empty
// fields are left out; a profile with nothing filled in gives "". The source holds public
// fields only: no email, phone, birthday or link can reach the text.
func ProfileText(src domain.ProfileSource) string {
	var lines []string
	add := func(s string) {
		if s = oneLine(s); s != "" {
			lines = append(lines, s)
		}
	}
	add(src.Headline)
	add(joinTags(src.Interests))
	add(joinTags(src.Skills))
	add(src.Bio)
	for _, p := range src.Publications {
		add(p.Title)
		add(p.Abstract)
	}
	return strings.Join(lines, "\n")
}

// ContentHash is the hex SHA-256 the worker compares with the stored one (AI-1.4).
func ContentHash(text string) string {
	sum := sha256.Sum256([]byte(textVersion + text))
	return hex.EncodeToString(sum[:])
}

func joinTags(tags []string) string {
	parts := make([]string, 0, len(tags))
	for _, t := range tags {
		if t = oneLine(t); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, ", ")
}

// oneLine collapses every run of whitespace, line breaks included, into one space.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// cutText returns at most domain.MaxTextChars characters of text, cut at a character.
func cutText(text string) string {
	if utf8.RuneCountInString(text) <= domain.MaxTextChars {
		return text
	}
	n := 0
	for i := range text {
		if n == domain.MaxTextChars {
			return text[:i]
		}
		n++
	}
	return text
}
