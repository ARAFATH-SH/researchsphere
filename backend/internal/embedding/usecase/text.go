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

// textVersion is hashed with every profile text, projectTextVersion with every project
// text. Changing how a text is built or split, without a new model, changes it, so every
// embedding of that owner type is made again.
const (
	textVersion        = "profile-text-v1\n"
	projectTextVersion = "project-text-v1\n"
	venueTextVersion   = "venue-text-v1\n"
	// projectAbstractTextVersion is hashed with every title-and-abstract text (AI-6.1).
	projectAbstractTextVersion = "project-abstract-text-v1\n"
)

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

// ProjectText is the one place a project's embedding text is built (AI-1.2): title, required
// skills, description, then abstract, each on its own line with its whitespace collapsed, so
// the same project always gives the same text and hash. Title and skills come first, so the
// AI service's split always puts them in the first chunk. The AI service adds the same
// "passage: " prefix and splits into chunks as for profiles.
func ProjectText(src domain.ProjectSource) string {
	var lines []string
	add := func(s string) {
		if s = oneLine(s); s != "" {
			lines = append(lines, s)
		}
	}
	add(src.Title)
	add(joinTags(src.Skills))
	add(src.Description)
	add(src.Abstract)
	return strings.Join(lines, "\n")
}

// ProjectAbstractText is the one place the text of a project's second vector is built, for
// venue recommendations (AI-6.1 says "title and abstract"): the title, then the abstract,
// each on its own line with its whitespace collapsed. It is empty when the abstract is empty,
// so a project without an abstract has no such vector (AI-6.3).
func ProjectAbstractText(src domain.ProjectSource) string {
	abstract := oneLine(src.Abstract)
	if abstract == "" {
		return ""
	}
	if title := oneLine(src.Title); title != "" {
		return title + "\n" + abstract
	}
	return abstract
}

// ContentHash is the hex SHA-256 of a profile text the worker compares with the stored one
// (AI-1.4).
func ContentHash(text string) string {
	sum := sha256.Sum256([]byte(textVersion + text))
	return hex.EncodeToString(sum[:])
}

// VenueText is the one place a venue's embedding text is built (AI-1.3): name, short name,
// research fields, then scope, each on its own line with its whitespace collapsed. The scope
// may hold a little Markdown; it is embedded as written. Removed venues have no text: the
// worker never asks for one.
func VenueText(src domain.VenueSource) string {
	var lines []string
	add := func(s string) {
		if s = oneLine(s); s != "" {
			lines = append(lines, s)
		}
	}
	add(src.Name)
	add(src.ShortName)
	add(joinTags(src.Fields))
	add(src.Scope)
	return strings.Join(lines, "\n")
}

// VenueContentHash is ContentHash for a venue text.
func VenueContentHash(text string) string {
	sum := sha256.Sum256([]byte(venueTextVersion + text))
	return hex.EncodeToString(sum[:])
}

// ProjectContentHash is ContentHash for a project text.
func ProjectContentHash(text string) string {
	sum := sha256.Sum256([]byte(projectTextVersion + text))
	return hex.EncodeToString(sum[:])
}

// ProjectAbstractContentHash is ContentHash for a project's title-and-abstract text.
func ProjectAbstractContentHash(text string) string {
	sum := sha256.Sum256([]byte(projectAbstractTextVersion + text))
	return hex.EncodeToString(sum[:])
}

// hashOf is the content hash of an owner's text.
func hashOf(owner domain.OwnerType, text string) string {
	switch owner {
	case domain.OwnerProject:
		return ProjectContentHash(text)
	case domain.OwnerVenue:
		return VenueContentHash(text)
	case domain.OwnerProjectAbstract:
		return ProjectAbstractContentHash(text)
	}
	return ContentHash(text)
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
