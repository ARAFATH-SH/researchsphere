package usecase

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
)

// Limits of an import file (FR-14.5; docs/venues-import.md).
const (
	MaxImportBytes = 1 << 20
	MaxImportRows  = 1000
	// maxDerivedNameChars keeps a derived key within MaxSourceKeyChars.
	maxDerivedNameChars = 180
)

// Outcomes of an import row.
const (
	OutcomeCreated   = "created"
	OutcomeUpdated   = "updated"
	OutcomeUnchanged = "unchanged"
	OutcomeRejected  = "rejected"
)

// ImportRow is what happened to one row of the file.
type ImportRow struct {
	Row       int // from 1, not counting a CSV header
	Outcome   string
	VenueID   string // "" when rejected, or created on a dry run
	SourceKey string
	Problems  []domain.Problem
}

// ImportResult is what an import did, or would do on a dry run.
type ImportResult struct {
	DryRun                                bool
	Created, Updated, Unchanged, Rejected int
	Rows                                  []ImportRow
}

// fileRow is one row of the file: a venue and its key. badRolling is a rolling cell that is
// not true, false or empty.
type fileRow struct {
	Input
	SourceKey  string
	badRolling bool
}

// columns are the CSV columns and JSON keys, as in VenueInput plus sourceKey.
var columns = []string{
	"sourceKey", "name", "shortName", "type", "rolling", "fields", "attendance", "city", "country", "startsOn",
	"endsOn", "timeZone", "submissionDeadline", "abstractDeadline", "notificationAt", "officialUrl", "scope",
}

// errDryRun rolls a dry run's transaction back.
var errDryRun = errors.New("dry run")

// Import adds and updates venues from a CSV or JSON file (FR-14.5, interim). Every accepted
// row is written in one transaction with its audit row; a dry run reports the same and
// writes nothing. A file that cannot be read as a whole is an ImportFileError,
// TooManyRowsError or ErrFileTooLarge.
func (s *Service) Import(ctx context.Context, adminID, filename string, r io.Reader, dryRun bool) (ImportResult, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxImportBytes+1))
	if err != nil {
		return ImportResult{}, err
	}
	if len(data) > MaxImportBytes {
		return ImportResult{}, domain.ErrFileTooLarge
	}
	rows, err := parseFile(filename, data)
	if err != nil {
		return ImportResult{}, err
	}
	known, err := s.catalog(ctx)
	if err != nil {
		return ImportResult{}, err
	}

	res := ImportResult{DryRun: dryRun, Rows: make([]ImportRow, len(rows))}
	keys := make([]string, len(rows))
	keyProblems := make([]*domain.Problem, len(rows))
	for i, row := range rows {
		keys[i], keyProblems[i] = importKey(row)
	}
	importID, err := newUUID()
	if err != nil {
		return ImportResult{}, err
	}

	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.store.LockImports(ctx); err != nil {
			return err
		}
		existing, err := s.store.ImportedByKeys(ctx, domain.SourceImport, nonEmpty(keys))
		if err != nil {
			return err
		}
		name, err := s.store.AdminName(ctx, adminID)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for i, row := range rows {
			out := ImportRow{Row: i + 1, SourceKey: keys[i]}
			v, problems := validate(row.Input, known)
			if row.badRolling {
				problems = append([]domain.Problem{{Field: "rolling", Code: domain.CodeInvalid}}, problems...)
			}
			if keyProblems[i] != nil {
				problems = append([]domain.Problem{*keyProblems[i]}, problems...)
			}
			if k := keys[i]; k != "" {
				if seen[k] {
					problems = append(problems, domain.Problem{Field: "sourceKey", Code: domain.CodeDuplicateInFile})
				}
				seen[k] = true
			}
			ex, found := existing[keys[i]]
			if len(problems) == 0 && found && ex.Removed {
				problems = append(problems, domain.Problem{Field: "row", Code: domain.CodeVenueRemoved})
			}
			if len(problems) > 0 {
				out.Outcome, out.Problems = OutcomeRejected, problems
				res.Rejected++
				res.Rows[i] = out
				continue
			}
			v.Source, v.SourceKey = domain.SourceImport, keys[i]
			switch {
			case found:
				out.VenueID = ex.Venue.ID
				changed := changedFields(ex.Venue, v)
				if len(changed) == 0 {
					out.Outcome = OutcomeUnchanged
					res.Unchanged++
					break
				}
				out.Outcome = OutcomeUpdated
				res.Updated++
				if dryRun {
					break
				}
				v.ID = ex.Venue.ID
				if err := s.store.Update(ctx, v); err != nil {
					return err
				}
				if err := s.store.Record(ctx, domain.Action{VenueID: v.ID, AdminID: adminID, AdminName: name,
					Kind: domain.ActImportUpdate, ChangedFields: changed, ImportID: importID}); err != nil {
					return err
				}
			default:
				out.Outcome = OutcomeCreated
				res.Created++
				if dryRun {
					break
				}
				id, err := s.store.Create(ctx, v, adminID)
				if err != nil {
					return err
				}
				out.VenueID = id
				if err := s.store.Record(ctx, domain.Action{VenueID: id, AdminID: adminID, AdminName: name,
					Kind: domain.ActImportCreate, ImportID: importID}); err != nil {
					return err
				}
			}
			res.Rows[i] = out
		}
		if dryRun {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return ImportResult{}, err
	}
	if !dryRun && res.Created+res.Updated > 0 {
		s.changed(ctx)
	}
	return res, nil
}

// importKey returns the row's key: its sourceKey, or "auto:" + the folded name + ":" + the
// year of startsOn (of the deadline when there is no startsOn); a rolling journal, which has
// no year, is "auto:" + the folded name. A key that cannot be made is "" with a problem.
func importKey(row fileRow) (string, *domain.Problem) {
	if k := strings.TrimSpace(row.SourceKey); k != "" {
		switch {
		case utf8.RuneCountInString(k) > MaxSourceKeyChars:
			return "", &domain.Problem{Field: "sourceKey", Code: domain.CodeTooLong}
		case hasControl(k, false):
			return "", &domain.Problem{Field: "sourceKey", Code: domain.CodeInvalid}
		}
		return k, nil
	}
	name := foldName(row.Name)
	if name == "" {
		// validate reports the missing name.
		return "", nil
	}
	if row.Rolling {
		if utf8.RuneCountInString(name) > maxDerivedNameChars {
			name = string([]rune(name)[:maxDerivedNameChars])
		}
		return "auto:" + name, nil
	}
	year := ""
	for _, s := range []string{row.StartsOn, row.SubmissionDeadline} {
		s = strings.TrimSpace(s)
		if len(s) >= len(dateLayout) {
			if d, err := parseDate(s[:len(dateLayout)]); err == nil {
				year = fmt.Sprintf("%04d", d.Year())
				break
			}
		}
	}
	if year == "" {
		// validate reports the missing or broken deadline.
		return "", nil
	}
	if utf8.RuneCountInString(name) > maxDerivedNameChars {
		name = string([]rune(name)[:maxDerivedNameChars])
	}
	return "auto:" + name + ":" + year, nil
}

// foldName lowercases a name and collapses its whitespace. Accents are kept (no library for
// that in the standard library); docs/venues-import.md says so.
func foldName(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func nonEmpty(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}

// parseFile reads the rows of a CSV or JSON file: by the name's extension, else JSON when the
// first non-blank character is [.
func parseFile(filename string, data []byte) ([]fileRow, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		return nil, &domain.ImportFileError{Message: "The file is not UTF-8 text."}
	}
	var (
		rows []fileRow
		err  error
	)
	switch ext := strings.ToLower(path.Ext(filename)); {
	case ext == ".json":
		rows, err = parseJSON(data)
	case ext == ".csv":
		rows, err = parseCSV(data)
	case bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")):
		rows, err = parseJSON(data)
	default:
		rows, err = parseCSV(data)
	}
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, &domain.ImportFileError{Message: "The file has no venues."}
	}
	return rows, nil
}

// jsonRow is one element of a JSON file.
type jsonRow struct {
	SourceKey          string   `json:"sourceKey"`
	Name               string   `json:"name"`
	ShortName          string   `json:"shortName"`
	Type               string   `json:"type"`
	Rolling            string   `json:"rolling"`
	Fields             []string `json:"fields"`
	Attendance         string   `json:"attendance"`
	City               string   `json:"city"`
	Country            string   `json:"country"`
	StartsOn           string   `json:"startsOn"`
	EndsOn             string   `json:"endsOn"`
	TimeZone           string   `json:"timeZone"`
	SubmissionDeadline string   `json:"submissionDeadline"`
	AbstractDeadline   string   `json:"abstractDeadline"`
	NotificationAt     string   `json:"notificationAt"`
	OfficialURL        string   `json:"officialUrl"`
	Scope              string   `json:"scope"`
}

func parseJSON(data []byte) ([]fileRow, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return nil, &domain.ImportFileError{Message: "A JSON file must be an array of venues."}
	}
	var rows []fileRow
	for dec.More() {
		if len(rows) == MaxImportRows {
			return nil, &domain.TooManyRowsError{Max: MaxImportRows}
		}
		var j jsonRow
		if err := dec.Decode(&j); err != nil {
			return nil, jsonError(err, len(rows)+1)
		}
		rolling, ok := parseRolling(j.Rolling)
		rows = append(rows, fileRow{SourceKey: j.SourceKey, badRolling: !ok, Input: Input{
			Name: j.Name, ShortName: j.ShortName, Type: j.Type, Rolling: rolling, Fields: j.Fields, Attendance: j.Attendance,
			City: j.City, Country: j.Country, StartsOn: j.StartsOn, EndsOn: j.EndsOn, TimeZone: j.TimeZone,
			SubmissionDeadline: j.SubmissionDeadline, AbstractDeadline: j.AbstractDeadline,
			NotificationAt: j.NotificationAt, OfficialURL: j.OfficialURL, Scope: j.Scope,
		}})
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim(']') {
		return nil, &domain.ImportFileError{Message: "The JSON array is not closed."}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, &domain.ImportFileError{Message: "The file has more after the JSON array."}
	}
	return rows, nil
}

// jsonError says what is wrong with element n without quoting the file: an unknown key is
// named, as a column would be.
func jsonError(err error, n int) error {
	const unknown = "json: unknown field "
	if msg := err.Error(); strings.HasPrefix(msg, unknown) {
		key := strings.Trim(strings.TrimPrefix(msg, unknown), `"`)
		if utf8.RuneCountInString(key) > 40 || hasControl(key, false) {
			key = "?"
		}
		return &domain.ImportFileError{Message: fmt.Sprintf("Venue %d has an unknown key %q.", n, key)}
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return &domain.ImportFileError{Message: fmt.Sprintf("Venue %d: %s must be text (fields: a list of text).", n, typeErr.Field)}
	}
	return &domain.ImportFileError{Message: fmt.Sprintf("Venue %d is not a valid JSON object.", n)}
}

func parseCSV(data []byte) ([]fileRow, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = 0
	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, &domain.ImportFileError{Message: "The file has no venues."}
	}
	if err != nil {
		return nil, csvError(err)
	}
	index := map[string]int{}
	for i, h := range header {
		name, ok := columnName(h)
		if !ok {
			col := strings.TrimSpace(h)
			if utf8.RuneCountInString(col) > 40 || hasControl(col, false) {
				col = "?"
			}
			return nil, &domain.ImportFileError{Message: fmt.Sprintf("Unknown column %q.", col)}
		}
		if _, dup := index[name]; dup {
			return nil, &domain.ImportFileError{Message: fmt.Sprintf("The column %s appears twice.", name)}
		}
		index[name] = i
	}
	var rows []fileRow
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, csvError(err)
		}
		if len(rows) == MaxImportRows {
			return nil, &domain.TooManyRowsError{Max: MaxImportRows}
		}
		get := func(col string) string {
			if i, ok := index[col]; ok {
				return rec[i]
			}
			return ""
		}
		var fields []string
		if f := get("fields"); strings.TrimSpace(f) != "" {
			fields = strings.Split(f, ";")
		}
		rolling, ok := parseRolling(get("rolling"))
		rows = append(rows, fileRow{SourceKey: get("sourceKey"), badRolling: !ok, Input: Input{
			Name: get("name"), ShortName: get("shortName"), Type: get("type"), Rolling: rolling, Fields: fields,
			Attendance: get("attendance"), City: get("city"), Country: get("country"),
			StartsOn: get("startsOn"), EndsOn: get("endsOn"), TimeZone: get("timeZone"),
			SubmissionDeadline: get("submissionDeadline"), AbstractDeadline: get("abstractDeadline"),
			NotificationAt: get("notificationAt"), OfficialURL: get("officialUrl"), Scope: get("scope"),
		}})
	}
	return rows, nil
}

// parseRolling reads a rolling cell: true or false in any letter case, empty for false.
func parseRolling(s string) (rolling, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "false":
		return false, true
	case "true":
		return true, true
	}
	return false, false
}

// columnName matches a header to a column, ignoring case and surrounding spaces.
func columnName(h string) (string, bool) {
	h = strings.TrimSpace(h)
	for _, c := range columns {
		if strings.EqualFold(h, c) {
			return c, true
		}
	}
	return "", false
}

// csvError names the line of a CSV error without quoting it.
func csvError(err error) error {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		if errors.Is(pe.Err, csv.ErrFieldCount) {
			return &domain.ImportFileError{Message: fmt.Sprintf("Line %d has a different number of cells than the header.", pe.Line)}
		}
		return &domain.ImportFileError{Message: fmt.Sprintf("Line %d is not valid CSV.", pe.Line)}
	}
	return &domain.ImportFileError{Message: "The file is not valid CSV."}
}

// newUUID returns a random (version 4) UUID.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
