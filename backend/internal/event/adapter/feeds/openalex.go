package feeds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
)

// DefaultOpenAlexBase is the OpenAlex API.
const DefaultOpenAlexBase = "https://api.openalex.org"

// Limits of an OpenAlex read.
const (
	openAlexPerPage  = 100 // the largest page OpenAlex supports
	openAlexMaxPages = 100 // page paging stops at 10,000 results
	maxPageBytes     = 4 << 20
	// DefaultMinWorks is the least number of works a journal must have.
	DefaultMinWorks = 5000
)

// openAlexSelect is every field asked for, and no more.
const openAlexSelect = "id,display_name,homepage_url,issn_l,host_organization_name,works_count,topics"

// OpenAlexConfig says what journals to ask OpenAlex for.
type OpenAlexConfig struct {
	BaseURL string // DefaultOpenAlexBase when empty
	// Mailto is sent as the mailto parameter and in the User-Agent; APIKey, when set, as
	// api_key (never logged).
	Mailto string
	APIKey string
	// MinWorks: journals with more works than this; Subfields: whose topics include one of
	// these subfields (the usecase keeps those whose main topic does).
	MinWorks  int
	Subfields []int
	// Now is the clock that sets the last publication year; nil means time.Now.
	Now func() time.Time
	// HTTP, Pace, Retries and Backoff override the defaults (tests).
	HTTP    *http.Client
	Pace    time.Duration
	Retries int
	Backoff time.Duration
}

// OpenAlex reads journals from OpenAlex's sources (CC0).
type OpenAlex struct {
	cfg OpenAlexConfig
	f   *fetcher
}

var _ domain.JournalFeed = (*OpenAlex)(nil)

// NewOpenAlex returns an OpenAlex feed.
func NewOpenAlex(cfg OpenAlexConfig) *OpenAlex {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultOpenAlexBase
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.MinWorks <= 0 {
		cfg.MinWorks = DefaultMinWorks
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	pace := cfg.Pace
	if pace == 0 {
		pace = DefaultPace
	}
	retries := cfg.Retries
	if retries == 0 {
		retries = DefaultRetries
	}
	return &OpenAlex{cfg: cfg, f: newFetcher(cfg.HTTP, UserAgent(cfg.Mailto), pace, retries, cfg.Backoff)}
}

// Filter is the OpenAlex filter the feed uses: core journals still publishing, with more
// than MinWorks works, in the subfields asked for.
func (o *OpenAlex) Filter() string {
	subs := make([]string, len(o.cfg.Subfields))
	for i, s := range o.cfg.Subfields {
		subs[i] = strconv.Itoa(s)
	}
	since := o.cfg.Now().UTC().Year() - 2
	return fmt.Sprintf("type:journal,is_core:true,works_count:>%d,last_publication_year:>%d,topics.subfield.id:%s",
		o.cfg.MinWorks, since, strings.Join(subs, "|"))
}

// URL is where the feed reads from, for the status: the query without any key.
func (o *OpenAlex) URL() string {
	return o.cfg.BaseURL + "/sources?filter=" + o.Filter()
}

type openAlexPage struct {
	Meta struct {
		Count int `json:"count"`
	} `json:"meta"`
	Results []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		HomepageURL string `json:"homepage_url"`
		ISSNL       string `json:"issn_l"`
		Host        string `json:"host_organization_name"`
		WorksCount  int    `json:"works_count"`
		Topics      []struct {
			DisplayName string `json:"display_name"`
			Subfield    struct {
				ID string `json:"id"`
			} `json:"subfield"`
			Field struct {
				ID string `json:"id"`
			} `json:"field"`
		} `json:"topics"`
	} `json:"results"`
}

// Journals reads every page of the filter, 100 journals a page, at the fetcher's pace.
// OpenAlex sends no ETag, so this is one full read a day: a few dozen requests.
func (o *OpenAlex) Journals(ctx context.Context) ([]domain.Journal, error) {
	if len(o.cfg.Subfields) == 0 {
		return nil, errors.New("no subfields to ask for")
	}
	var out []domain.Journal
	for page := 1; page <= openAlexMaxPages; page++ {
		q := url.Values{
			"filter":   {o.Filter()},
			"select":   {openAlexSelect},
			"per_page": {strconv.Itoa(openAlexPerPage)},
			"page":     {strconv.Itoa(page)},
			"sort":     {"works_count:desc"},
		}
		if o.cfg.Mailto != "" {
			q.Set("mailto", o.cfg.Mailto)
		}
		if o.cfg.APIKey != "" {
			q.Set("api_key", o.cfg.APIKey)
		}
		resp, err := o.f.get(ctx, o.cfg.BaseURL+"/sources?"+q.Encode(), map[string]string{"Accept": "application/json"}, maxPageBytes)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", page, err)
		}
		var p openAlexPage
		if err := json.Unmarshal(resp.Body, &p); err != nil {
			return nil, fmt.Errorf("page %d: not a page of sources", page)
		}
		for _, r := range p.Results {
			j := domain.Journal{
				ID: lastSegment(r.ID), Name: r.DisplayName, HomepageURL: r.HomepageURL, ISSNL: r.ISSNL,
				Publisher: r.Host, WorksCount: r.WorksCount,
			}
			for _, t := range r.Topics {
				sub, _ := strconv.Atoi(lastSegment(t.Subfield.ID))
				field, _ := strconv.Atoi(lastSegment(t.Field.ID))
				j.Topics = append(j.Topics, domain.JournalTopic{Name: t.DisplayName, SubfieldID: sub, FieldID: field})
			}
			out = append(out, j)
		}
		if len(p.Results) < openAlexPerPage || page*openAlexPerPage >= p.Meta.Count {
			return out, nil
		}
	}
	return out, nil
}

// lastSegment is the ID at the end of an OpenAlex URL: S137773608 of
// https://openalex.org/S137773608.
func lastSegment(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}
