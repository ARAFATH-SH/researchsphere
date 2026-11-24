package usecase

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
)

// Journals from OpenAlex, mapped to rolling venues (FR-14.5; docs/venues-sync.md).

// subfieldFields maps OpenAlex topic subfields (https://openalex.org/subfields/<id>) to
// research fields. A journal is imported only when its main topic's subfield is listed.
var subfieldFields = map[int]string{
	1702: "explainable_ai",             // Artificial Intelligence
	1707: "explainable_ai",             // Computer Vision and Pattern Recognition
	1705: "wireless_networks",          // Computer Networks and Communications
	2741: "medical_imaging",            // Radiology, Nuclear Medicine and Imaging
	2713: "public_health_epidemiology", // Epidemiology
	2739: "public_health_epidemiology", // Public Health, Environmental and Occupational Health
	2738: "mental_health",              // Psychiatry and Mental health
	3203: "mental_health",              // Clinical Psychology
	1311: "genomics_bioinformatics",    // Genetics
	3003: "drug_discovery",             // Pharmaceutical Science
	3004: "drug_discovery",             // Pharmacology
	2736: "drug_discovery",             // Pharmacology (medical)
	2312: "water_resources",            // Water Science and Technology
	2306: "climate_modelling",          // Global and Planetary Change
	1902: "climate_modelling",          // Atmospheric Science
	2205: "structural_engineering",     // Civil and Structural Engineering
	3313: "urban_transport",            // Transportation
	3322: "urban_transport",            // Urban Studies
	3304: "education_technology",       // Education
	3303: "development_economics",      // Development
	2002: "development_economics",      // Economics and Econometrics
	1910: "marine_fisheries",           // Oceanography
	1104: "marine_fisheries",           // Aquatic Science
	1102: "agri_remote_sensing",        // Agronomy and Crop Science
	1111: "agri_remote_sensing",        // Soil Science
	1904: "agri_remote_sensing",        // Earth-Surface Processes
	2105: "renewable_energy",           // Renewable Energy, Sustainability and the Environment
	2102: "renewable_energy",           // Energy Engineering and Power Technology
	2207: "robotics",                   // Control and Systems Engineering
	2500: "materials_science",          // General Materials Science
	2502: "materials_science",          // Biomaterials
	2503: "materials_science",          // Ceramics and Composites
	2504: "materials_science",          // Electronic, Optical and Magnetic Materials
	2505: "materials_science",          // Materials Chemistry
	2506: "materials_science",          // Metals and Alloys
	2507: "materials_science",          // Polymers and Plastics
	2508: "materials_science",          // Surfaces, Coatings and Films
}

// JournalSubfields lists the subfields the journal feed asks OpenAlex for, in order.
func JournalSubfields() []int {
	ids := make([]int, 0, len(subfieldFields))
	for id := range subfieldFields {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// journalTopics is how many of a journal's main topics give it fields.
const journalTopics = 3

// issnPortal is the link of a journal's ISSN portal record.
const issnPortal = "https://portal.issn.org/resource/ISSN/"

// mapJournal turns a journal into a candidate, without its link (resolveLinks sets it). keep
// is false for a journal whose main topic is not in a listed subfield.
func mapJournal(j domain.Journal) (c candidate, keep bool) {
	if len(j.Topics) == 0 {
		return candidate{}, false
	}
	if _, ok := subfieldFields[j.Topics[0].SubfieldID]; !ok {
		return candidate{}, false
	}
	var fields []string
	for _, t := range j.Topics[:min(len(j.Topics), journalTopics)] {
		if k, ok := subfieldFields[t.SubfieldID]; ok && !slices.Contains(fields, k) {
			fields = append(fields, k)
		}
	}
	id := strings.TrimSpace(j.ID)
	c.key = string(domain.SourceOpenAlex) + ":" + id
	if id == "" || utf8.RuneCountInString(c.key) > MaxSourceKeyChars || hasControl(c.key, false) {
		c.key = ""
		c.problems = append(c.problems, domain.Problem{Field: "sourceKey", Code: domain.CodeInvalid})
	}
	c.name = strings.TrimSpace(j.Name)
	c.in = Input{Name: c.name, Type: string(domain.TypeJournal), Rolling: true, Fields: fields, Scope: journalScope(j)}
	return c, true
}

// journalScope is the scope text of a journal: its main topics and publisher.
func journalScope(j domain.Journal) string {
	var names []string
	for _, t := range j.Topics[:min(len(j.Topics), 5)] {
		if n := strings.TrimSpace(t.Name); n != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(j.Name) + " is a journal that takes submissions at any time.")
	if len(names) > 0 {
		b.WriteString("\n\n**Main topics:** " + strings.Join(names, "; ") + ".")
	}
	if p := strings.TrimSpace(j.Publisher); p != "" {
		b.WriteString("\n\n**Publisher:** " + p + ".")
	}
	if j.ISSNL != "" {
		b.WriteString("\n\n**ISSN-L:** " + j.ISSNL + ".")
	}
	b.WriteString("\n\nImported from OpenAlex.")
	return strings.Map(func(r rune) rune {
		if r == '\r' || (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return -1
		}
		return r
	}, b.String())
}

// httpsLink is a homepage as https: http:// is rewritten, anything else but https:// is "".
func httpsLink(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(strings.ToLower(s), "https://"):
		return "https://" + s[len("https://"):]
	case strings.HasPrefix(strings.ToLower(s), "http://"):
		return "https://" + s[len("http://"):]
	}
	return ""
}

// validISSN is an ISSN such as 0028-0836 (the last character may be X).
func validISSN(s string) bool {
	if len(s) != 9 || s[4] != '-' {
		return false
	}
	for i, r := range s {
		switch {
		case i == 4:
		case r >= '0' && r <= '9':
		case i == 8 && r == 'X':
		default:
			return false
		}
	}
	return true
}

// resolveLinks sets each journal's link (docs/decisions.md, 2026-11-24): its homepage as
// https when that answers (checked at most every recheck, concurrency at a time), else its
// ISSN portal record, marked issn_portal. A journal with neither is rejected. It returns the
// checks made this time.
func resolveLinks(ctx context.Context, cands []candidate, homepages []string, issns []string, cached map[string]domain.LinkCheck,
	checker domain.LinkChecker, now time.Time, recheck time.Duration, concurrency int,
) map[string]domain.LinkCheck {
	fresh := map[string]domain.LinkCheck{}
	var todo []string
	for _, u := range homepages {
		if u == "" || slices.Contains(todo, u) {
			continue
		}
		if c, ok := cached[u]; ok && now.Sub(c.CheckedAt) < recheck {
			continue
		}
		todo = append(todo, u)
	}
	if concurrency < 1 {
		concurrency = 1
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, concurrency)
	)
	for _, u := range todo {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			c := checker.Check(ctx, u)
			if ctx.Err() != nil {
				// Cut short by the run's end, not an answer: neither kept nor trusted.
				return
			}
			if c.CheckedAt.IsZero() {
				c.CheckedAt = now
			}
			mu.Lock()
			fresh[u] = c
			mu.Unlock()
		}()
	}
	wg.Wait()

	for i := range cands {
		u := homepages[i]
		check, ok := fresh[u]
		if !ok {
			check, ok = cached[u]
		}
		switch {
		case u != "" && ok && check.OK:
			cands[i].in.OfficialURL, cands[i].linkKind = u, domain.LinkOfficial
		case validISSN(issns[i]):
			cands[i].in.OfficialURL, cands[i].linkKind = issnPortal+issns[i], domain.LinkISSNPortal
		default:
			cands[i].problems = append(cands[i].problems, domain.Problem{Field: "officialUrl", Code: domain.CodeRequired})
		}
	}
	return fresh
}
