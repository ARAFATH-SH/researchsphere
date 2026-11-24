package feeds

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
)

// The data files of the deadline repositories (docs/venues-sync.md). Values the
// repositories write loosely (a year as text, an area as one code or a list) are read
// either way; a file that is not this shape is reported, never guessed at.

// flexStrings is one string or a list of strings.
type flexStrings []string

func (f *flexStrings) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			*f = nil
			return nil
		}
		*f = flexStrings{n.Value}
		return nil
	case yaml.SequenceNode:
		var out []string
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: a list of text was expected", c.Line)
			}
			out = append(out, c.Value)
		}
		*f = out
		return nil
	}
	return fmt.Errorf("line %d: text or a list of text was expected", n.Line)
}

// flexInt is a whole number, also when written as text.
type flexInt int

func (f *flexInt) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: a number was expected", n.Line)
	}
	if n.Tag == "!!null" || strings.TrimSpace(n.Value) == "" {
		*f = 0
		return nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(n.Value))
	if err != nil {
		return fmt.Errorf("line %d: a number was expected", n.Line)
	}
	*f = flexInt(v)
	return nil
}

// text is a scalar as written: a date such as 2027-05-10 stays as it is, a number too.
type text string

func (t *text) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: text was expected", n.Line)
	}
	if n.Tag == "!!null" {
		*t = ""
		return nil
	}
	*t = text(n.Value)
	return nil
}

// ccfSeries is one conference series of ccf-deadlines.
type ccfSeries struct {
	Title       text            `yaml:"title"`
	Description text            `yaml:"description"`
	Sub         flexStrings     `yaml:"sub"`
	Rank        map[string]text `yaml:"rank"`
	Confs       []ccfConf       `yaml:"confs"`
}

type ccfConf struct {
	Year     flexInt    `yaml:"year"`
	ID       text       `yaml:"id"`
	Link     text       `yaml:"link"`
	Timeline []ccfRound `yaml:"timeline"`
	Timezone text       `yaml:"timezone"`
	Date     text       `yaml:"date"`
	Place    text       `yaml:"place"`
}

type ccfRound struct {
	Deadline         text `yaml:"deadline"`
	AbstractDeadline text `yaml:"abstract_deadline"`
}

// rankOrder is the order ranks are named in.
var rankOrder = []string{"ccf", "core", "thcpl"}

func ranksOf(m map[string]text) string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		ia, ib := slices.Index(rankOrder, a), slices.Index(rankOrder, b)
		if ia < 0 {
			ia = len(rankOrder)
		}
		if ib < 0 {
			ib = len(rankOrder)
		}
		if ia != ib {
			return ia - ib
		}
		return strings.Compare(a, b)
	})
	var parts []string
	for _, k := range keys {
		v := strings.TrimSpace(string(m[k]))
		if v == "" || strings.EqualFold(v, "N") {
			continue
		}
		parts = append(parts, strings.ToUpper(k)+" "+v)
	}
	return strings.Join(parts, ", ")
}

func parseCCF(files []domain.RepoFile) ([]domain.Edition, []domain.FileError) {
	var (
		out  []domain.Edition
		errs []domain.FileError
	)
	for _, f := range files {
		var series []ccfSeries
		if err := yaml.Unmarshal([]byte(f.Content), &series); err != nil {
			errs = append(errs, domain.FileError{Path: f.Path, Message: yamlMessage(err)})
			continue
		}
		for _, s := range series {
			for _, c := range s.Confs {
				e := domain.Edition{
					File: f.Path, ID: string(c.ID), Title: strings.TrimSpace(string(s.Title)),
					FullName: strings.TrimSpace(string(s.Description)), Year: int(c.Year), Link: strings.TrimSpace(string(c.Link)),
					Areas: s.Sub, Ranks: ranksOf(s.Rank), TimeZone: string(c.Timezone), Place: string(c.Place),
				}
				for _, r := range c.Timeline {
					e.Rounds = append(e.Rounds, domain.Round{Deadline: string(r.Deadline), AbstractDeadline: string(r.AbstractDeadline)})
				}
				out = append(out, e)
			}
		}
	}
	return out, errs
}

// hciConf is one edition of hci-deadlines' conf-database.
type hciConf struct {
	Title            text        `yaml:"title"`
	Year             flexInt     `yaml:"year"`
	ID               text        `yaml:"id"`
	FullName         text        `yaml:"full_name"`
	Link             text        `yaml:"link"`
	Deadline         flexStrings `yaml:"deadline"`
	AbstractDeadline text        `yaml:"abstract_deadline"`
	Timezone         text        `yaml:"timezone"`
	Place            text        `yaml:"place"`
	Start            text        `yaml:"start"`
	End              text        `yaml:"end"`
	Sub              flexStrings `yaml:"sub"`
	Note             text        `yaml:"note"`
}

func parseHCI(files []domain.RepoFile) ([]domain.Edition, []domain.FileError) {
	var (
		out  []domain.Edition
		errs []domain.FileError
	)
	for _, f := range files {
		var confs []hciConf
		if err := yaml.Unmarshal([]byte(f.Content), &confs); err != nil {
			errs = append(errs, domain.FileError{Path: f.Path, Message: yamlMessage(err)})
			continue
		}
		for _, c := range confs {
			e := domain.Edition{
				File: f.Path, ID: string(c.ID), Title: strings.TrimSpace(string(c.Title)),
				FullName: strings.TrimSpace(string(c.FullName)), Year: int(c.Year), Link: strings.TrimSpace(string(c.Link)),
				Areas: c.Sub, TimeZone: string(c.Timezone), Place: string(c.Place),
				StartsOn: string(c.Start), EndsOn: string(c.End), Note: string(c.Note),
			}
			// The abstract deadline belongs to the paper deadline it comes before: the last.
			for i, d := range c.Deadline {
				r := domain.Round{Deadline: d}
				if i == len(c.Deadline)-1 {
					r.AbstractDeadline = string(c.AbstractDeadline)
				}
				e.Rounds = append(e.Rounds, r)
			}
			out = append(out, e)
		}
	}
	return out, errs
}

// yamlMessage says what is wrong with a file without quoting it at length.
func yamlMessage(err error) string {
	msg := "not valid YAML of the expected shape: " + err.Error()
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:300])
	}
	return msg
}
