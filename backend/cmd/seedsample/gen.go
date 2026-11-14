package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The vocabulary: one entry per research cluster, and the names, institutions and noise tags
// all clusters share. Everything in it is fictional; see docs/decisions.md, 2026-11-15.
//
//go:embed content/clusters.json content/common.json
var contentFS embed.FS

// cluster is one research topic with its own vocabulary. Templates use the slots of fill.
type cluster struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Interests   []string `json:"interests"`
	Skills      []string `json:"skills"`
	Departments []string `json:"departments"`
	Headlines   []string `json:"headlines"`
	Bio         []string `json:"bio"`
	Methods     []string `json:"methods"`
	Objects     []string `json:"objects"`
	Contexts    []string `json:"contexts"`
	Titles      []string `json:"titles"`
	Abstracts   []string `json:"abstracts"`
	Venues      []string `json:"venues"`
	BnInterests []string `json:"bn_interests"`
	BnHeadlines []string `json:"bn_headlines"`
	BnBio       []string `json:"bn_bio"`
}

type common struct {
	BDFirstNames   []string `json:"bd_first_names"`
	BDLastNames    []string `json:"bd_last_names"`
	IntlFirstNames []string `json:"intl_first_names"`
	IntlLastNames  []string `json:"intl_last_names"`
	Institutions   []string `json:"institutions"`
	Roles          []string `json:"roles"`
	BnRoles        []string `json:"bn_roles"`
	Countries      []string `json:"countries"`
	BDCities       []string `json:"bd_cities"`
	NoiseInterests []string `json:"noise_interests"`
	NoiseSkills    []string `json:"noise_skills"`
}

type content struct {
	clusters []cluster
	common   common
}

func loadContent() (*content, error) {
	var c content
	for _, f := range []struct {
		path string
		into any
	}{{"content/clusters.json", &c.clusters}, {"content/common.json", &c.common}} {
		b, err := contentFS.ReadFile(f.path)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, f.into); err != nil {
			return nil, fmt.Errorf("%s: %w", f.path, err)
		}
	}
	if len(c.clusters) < 2 {
		return nil, fmt.Errorf("content has %d clusters, want at least 2", len(c.clusters))
	}
	return &c, nil
}

// The shares the generator aims for. Each is drawn per profile, so a run's share is close to,
// not exactly, these.
const (
	secondClusterShare = 0.30 // interdisciplinary: a second cluster
	testShare          = 0.20 // held out for NFR-6
	sparseShare        = 0.15 // headline and 2 interests only, like a new user
	banglaShare        = 0.15 // Bangla or mixed Bangla-English text
	fullBanglaShare    = 0.05 // of all profiles: Bangla only (the rest of banglaShare is mixed)
	noiseShare         = 0.10 // one or two tags from outside the clusters
	sharedPubShare     = 0.10 // a publication shared with an earlier author of the same cluster
	sameClusterLink    = 0.80 // a connection within the main cluster
)

// Split and language values, as stored in sample_profiles.
const (
	splitTuning = "tuning"
	splitTest   = "test"
	langNone    = "none"
	langMixed   = "mixed"
	langBangla  = "bangla"
)

// label is a profile's ground truth. second is -1 when there is none.
type label struct {
	main, second int
	split        string
	sparse       bool
	bangla       string
}

type publication struct {
	title, abstract, venue string
	year                   int
	coAuthor               int // seq of an earlier sample author, or -1
}

// profile is one generated sample profile. Empty strings are stored as NULL.
type profile struct {
	seq                                     int
	email, fullName, headline, bio          string
	institution, department, jobTitle, city string
	country                                 string
	interests, skills                       []string
	publications                            []publication
	connections                             []int // seqs of earlier profiles, accepted
	label                                   label
}

// generator makes profile seq from (seed, seq) alone, so a run that tops up 1,000 to 10,000
// makes the same profiles as one run of 10,000. Each profile has three random streams: its
// label, its text, and its links to earlier profiles (shared publications, connections),
// which read only the labels of lower seqs.
type generator struct {
	c    *content
	seed int64 // never negative: parseFlags refuses it
}

const (
	streamLabel = iota
	streamText
	streamLinks
	streamCount
)

func (g *generator) rng(seq, stream int) *rand.Rand {
	return rand.New(rand.NewPCG(uint64(g.seed), uint64(seq)*streamCount+uint64(stream))) //nolint:gosec // seed, seq and stream are not negative
}

func (g *generator) label(seq int) label {
	r := g.rng(seq, streamLabel)
	n := len(g.c.clusters)
	l := label{main: r.IntN(n), second: -1, split: splitTuning, bangla: langNone}
	if r.Float64() < secondClusterShare {
		s := r.IntN(n - 1)
		if s >= l.main {
			s++
		}
		l.second = s
	}
	if r.Float64() < testShare {
		l.split = splitTest
	}
	l.sparse = r.Float64() < sparseShare
	switch x := r.Float64(); {
	case x < fullBanglaShare:
		l.bangla = langBangla
	case x < banglaShare:
		l.bangla = langMixed
	}
	return l
}

func email(seq int) string { return fmt.Sprintf("sample-%05d@%s", seq, sampleDomain) }

func (g *generator) profile(seq int) profile {
	l := g.label(seq)
	r := g.rng(seq, streamText)
	cm := &g.c.common
	main := &g.c.clusters[l.main]
	var second *cluster
	if l.second >= 0 {
		second = &g.c.clusters[l.second]
	}
	p := profile{seq: seq, email: email(seq), label: l}

	bdName := r.Float64() < 0.7
	switch {
	case bdName:
		p.fullName = pick(r, cm.BDFirstNames) + " " + pick(r, cm.BDLastNames)
	case r.Float64() < 0.65:
		p.fullName = pick(r, cm.IntlFirstNames) + " " + pick(r, cm.IntlLastNames)
	default:
		p.fullName = pick(r, cm.BDFirstNames) + " " + pick(r, cm.IntlLastNames)
	}
	role := pick(r, cm.Roles)
	bnRole := pick(r, cm.BnRoles)

	// Interests first: headlines and bios name them.
	if l.bangla == langBangla {
		p.interests = pickN(r, main.BnInterests, 2+r.IntN(2))
	} else {
		p.interests = pickN(r, main.Interests, 4+r.IntN(5))
	}
	f := newFiller(r, main, role, bnRole)
	// A sparse mixed profile has only its headline to carry the Bangla.
	if l.bangla == langBangla || (l.bangla == langMixed && (l.sparse || r.Float64() < 0.5)) {
		p.headline = f.fill(pick(r, main.BnHeadlines))
	} else {
		p.headline = f.fill(pick(r, main.Headlines))
	}

	if l.sparse {
		p.interests = p.interests[:2]
		return p
	}

	if second != nil {
		if l.bangla == langBangla {
			p.interests = append(p.interests, pick(r, second.BnInterests))
		} else {
			p.interests = append(p.interests, pickN(r, second.Interests, 2+r.IntN(2))...)
		}
	}
	p.skills = pickN(r, main.Skills, 3+r.IntN(4))
	if second != nil {
		p.skills = append(p.skills, pickN(r, second.Skills, 1+r.IntN(2))...)
	}
	if r.Float64() < noiseShare {
		// The noise interests are English, so a Bangla-only profile gets only the skill.
		if l.bangla != langBangla {
			p.interests = append(p.interests, pickN(r, cm.NoiseInterests, 1+r.IntN(2))...)
		}
		p.skills = append(p.skills, pick(r, cm.NoiseSkills))
	}
	p.interests = dedupe(p.interests)
	p.skills = dedupe(p.skills)

	var sentences []string
	switch l.bangla {
	case langBangla:
		sentences = fillAll(f, pickN(r, main.BnBio, 2+r.IntN(3)))
		if second != nil {
			sentences = append(sentences, newFiller(r, second, role, bnRole).fill(pick(r, second.BnBio)))
		}
	case langMixed:
		sentences = fillAll(f, pickN(r, main.BnBio, 1+r.IntN(2)))
		sentences = append(sentences, fillAll(f, pickN(r, main.Bio, 1+r.IntN(2)))...)
	default:
		sentences = fillAll(f, pickN(r, main.Bio, 2+r.IntN(3)))
		if second != nil {
			sentences = append(sentences, newFiller(r, second, role, bnRole).fill(pick(r, second.Bio)))
		}
	}
	p.bio = strings.Join(sentences, " ")

	p.institution = pick(r, cm.Institutions)
	p.department = pick(r, main.Departments)
	p.jobTitle = role
	if bdName && r.Float64() < 0.85 || !bdName && r.Float64() < 0.2 {
		p.country = "BD"
		p.city = pick(r, cm.BDCities)
	} else {
		p.country = pick(r, cm.Countries)
	}

	// 0 to 5 publications, mostly from the main cluster. Titles and abstracts are English.
	counts := []float64{0.20, 0.20, 0.20, 0.15, 0.15, 0.10}
	nPubs := weighted(r, counts)
	links := g.rng(seq, streamLinks)
	for range nPubs {
		c := main
		if second != nil && r.Float64() < 0.3 {
			c = second
		}
		pf := newFiller(r, c, role, bnRole)
		pub := publication{
			title:    upperFirst(pf.fill(pick(r, c.Titles))),
			abstract: pf.fill(pick(r, c.Abstracts)),
			venue:    pick(r, c.Venues),
			year:     2015 + r.IntN(12),
			coAuthor: -1,
		}
		if links.Float64() < sharedPubShare {
			pub.coAuthor = g.earlierInCluster(links, seq, l.main)
		}
		p.publications = append(p.publications, pub)
	}

	// 0 to 3 connections to earlier profiles, mostly within the main cluster.
	seen := map[int]bool{}
	for range links.IntN(4) {
		j := -1
		if links.Float64() < sameClusterLink {
			j = g.earlierInCluster(links, seq, l.main)
		} else if seq > 0 {
			j = links.IntN(seq)
		}
		if j >= 0 && !seen[j] {
			seen[j] = true
			p.connections = append(p.connections, j)
		}
	}
	return p
}

// earlierInCluster returns a random seq below seq whose main cluster is c, or -1 when a few
// tries find none (the first profiles have few earlier ones).
func (g *generator) earlierInCluster(r *rand.Rand, seq, c int) int {
	if seq == 0 {
		return -1
	}
	for range 60 {
		j := r.IntN(seq)
		if g.label(j).main == c {
			return j
		}
	}
	return -1
}

// filler fills template slots from one cluster's vocabulary: {role}, {bn_role}, {interest},
// {interest2} (another interest than the last {interest}), {bn_interest}, {skill}, {method},
// {object} and {context}. A filler keeps the method, object and context it drew first, so a
// publication's title and abstract, or a bio's sentences, are about the same thing.
type filler struct {
	r            *rand.Rand
	c            *cluster
	role, bnRole string
	lastInterest string
	kept         map[string]string
}

func newFiller(r *rand.Rand, c *cluster, role, bnRole string) *filler {
	return &filler{r: r, c: c, role: role, bnRole: bnRole, kept: map[string]string{}}
}

func (f *filler) slot(name string) string {
	switch name {
	case "role":
		return f.role
	case "bn_role":
		return f.bnRole
	case "interest":
		f.lastInterest = pick(f.r, f.c.Interests)
		return f.lastInterest
	case "interest2":
		for {
			if s := pick(f.r, f.c.Interests); s != f.lastInterest {
				return s
			}
		}
	case "bn_interest":
		return pick(f.r, f.c.BnInterests)
	case "skill":
		return pick(f.r, f.c.Skills)
	case "method":
		return f.keep(name, f.c.Methods)
	case "object":
		return f.keep(name, f.c.Objects)
	case "context":
		return f.keep(name, f.c.Contexts)
	}
	panic("seedsample: unknown template slot {" + name + "}") // content_test checks every slot
}

func (f *filler) keep(slot string, list []string) string {
	if v, ok := f.kept[slot]; ok {
		return v
	}
	v := pick(f.r, list)
	f.kept[slot] = v
	return v
}

func (f *filler) fill(tmpl string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(tmpl, '{')
		if i < 0 {
			b.WriteString(tmpl)
			break
		}
		j := strings.IndexByte(tmpl[i:], '}')
		if j < 0 {
			b.WriteString(tmpl)
			break
		}
		b.WriteString(tmpl[:i])
		b.WriteString(f.slot(tmpl[i+1 : i+j]))
		tmpl = tmpl[i+j+1:]
	}
	return upperFirst(b.String())
}

func fillAll(f *filler, tmpls []string) []string {
	out := make([]string, len(tmpls))
	for i, t := range tmpls {
		out[i] = f.fill(t)
	}
	return out
}

func pick(r *rand.Rand, list []string) string { return list[r.IntN(len(list))] }

// pickN returns n distinct entries of list (all of them when n is larger), in random order.
func pickN(r *rand.Rand, list []string, n int) []string {
	idx := r.Perm(len(list))
	n = min(n, len(list))
	out := make([]string, n)
	for i := range n {
		out[i] = list[idx[i]]
	}
	return out
}

// weighted returns an index of weights, drawn in proportion to them.
func weighted(r *rand.Rand, weights []float64) int {
	x := r.Float64()
	for i, w := range weights {
		if x < w {
			return i
		}
		x -= w
	}
	return len(weights) - 1
}

// dedupe drops later entries equal ignoring letter case, as the citext tag tables do.
func dedupe(list []string) []string {
	seen := map[string]bool{}
	out := list[:0]
	for _, s := range list {
		k := strings.ToLower(s)
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError || !unicode.IsLower(r) {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
