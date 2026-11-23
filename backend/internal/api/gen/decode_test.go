package gen

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

type inner struct {
	Year int `json:"year"`
}

type item struct {
	Name string `json:"name"`
}

// body has the shapes oapi-codegen generates: a required field has no omitempty, also when
// it is nullable; an optional one is a pointer or a nullable.Nullable with omitempty.
type body struct {
	ID       openapi_types.UUID        `json:"id"`
	Name     string                    `json:"name"`
	Note     *string                   `json:"note"`
	Age      *int                      `json:"age,omitempty"`
	Active   *bool                     `json:"active,omitempty"`
	Score    *float32                  `json:"score,omitempty"`
	Tags     *[]string                 `json:"tags,omitempty"`
	Inner    *inner                    `json:"inner,omitempty"`
	Items    *[]item                   `json:"items,omitempty"`
	Headline nullable.Nullable[string] `json:"headline,omitempty"`
	Limit    nullable.Nullable[int]    `json:"limit,omitempty"`
	Other    *openapi_types.UUID       `json:"other,omitempty"`
}

const (
	validID = "7f3a1c2e-1111-4e8b-b7c1-2d3e4f5a6b7c"
	head    = `"id":"` + validID + `","name":"Ada","note":null`
)

func TestDecodeJSONBodyFillsTheStruct(t *testing.T) {
	var got body
	in := `{` + head + `,"age":30,"active":true,"score":1.5,"tags":["a","b"],"inner":{"year":2020},` +
		`"items":[{"name":"x"}],"headline":null,"limit":5,"other":"` + validID + `"}`
	if err := decodeJSONBody(strings.NewReader(in), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID.String() != validID || got.Name != "Ada" || got.Note != nil || *got.Age != 30 || !*got.Active ||
		*got.Score != 1.5 || len(*got.Tags) != 2 || got.Inner.Year != 2020 || (*got.Items)[0].Name != "x" ||
		!got.Headline.IsNull() || got.Limit.MustGet() != 5 || got.Other.String() != validID {
		t.Errorf("got %+v", got)
	}
}

func TestDecodeJSONBodyLeavesOptionalFieldsUnset(t *testing.T) {
	var got body
	if err := decodeJSONBody(strings.NewReader(`{`+head+`}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Age != nil || got.Tags != nil || got.Inner != nil || got.Headline.IsSpecified() {
		t.Errorf("got %+v", got)
	}
}

func TestDecodeJSONBodyNamesTheField(t *testing.T) {
	tests := []struct {
		name, in, field, problem string
	}{
		{"bad UUID", `{"id":"c1","name":"Ada","note":null}`, "id", "must be a UUID"},
		{"UUID of the wrong type", `{"id":5,"name":"Ada","note":null}`, "id", "must be a UUID"},
		{"empty UUID", `{"id":"","name":"Ada","note":null}`, "id", "must be a UUID"},
		{"null for a required UUID", `{"id":null,"name":"Ada","note":null}`, "id", "must be a UUID"},
		{"bad optional UUID", `{` + head + `,"other":"c1"}`, "other", "must be a UUID"},
		{"number for a string", `{"id":"` + validID + `","name":5,"note":null}`, "name", "must be a string"},
		{"null for a required string", `{"id":"` + validID + `","name":null,"note":null}`, "name", "must be a string"},
		{"string for a whole number", `{` + head + `,"age":"thirty"}`, "age", "must be a whole number"},
		{"fraction for a whole number", `{` + head + `,"age":1.5}`, "age", "must be a whole number"},
		{"string for a boolean", `{` + head + `,"active":"yes"}`, "active", "must be true or false"},
		{"string for a number", `{` + head + `,"score":"high"}`, "score", "must be a number"},
		{"string for a list", `{` + head + `,"tags":"a"}`, "tags", "must be a list"},
		{"number in a list", `{` + head + `,"tags":["a",5]}`, "tags[1]", "must be a string"},
		{"string for an object", `{` + head + `,"inner":"x"}`, "inner", "must be an object"},
		{"nested field", `{` + head + `,"inner":{"year":"then"}}`, "inner.year", "must be a whole number"},
		{"field of a list item", `{` + head + `,"items":[{"name":"a"},{"name":1}]}`, "items[1].name", "must be a string"},
		{"nullable of the wrong type", `{` + head + `,"headline":5}`, "headline", "must be a string"},
		{"nullable whole number", `{` + head + `,"limit":"x"}`, "limit", "must be a whole number"},
		{"missing required", `{"id":"` + validID + `","note":null}`, "name", "is required"},
		{"missing required nullable", `{"id":"` + validID + `","name":"Ada"}`, "note", "is required"},
		{"missing nested", `{` + head + `,"inner":{}}`, "inner.year", "is required"},
		{"missing in a list item", `{` + head + `,"items":[{}]}`, "items[0].name", "is required"},
		{"first in contract order", `{"name":5}`, "id", "is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got body
			err := decodeJSONBody(strings.NewReader(tt.in), &got)
			// The generated handlers wrap the error before passing it on.
			var fe *FieldError
			if !errors.As(fmt.Errorf("can't decode JSON body: %w", err), &fe) {
				t.Fatalf("err = %v, want a FieldError", err)
			}
			if fe.Field != tt.field || fe.Problem != tt.problem {
				t.Errorf("got %q %q, want %q %q", fe.Field, fe.Problem, tt.field, tt.problem)
			}
			if want := tt.field + " " + tt.problem + "."; fe.Error() != want {
				t.Errorf("Error() = %q, want %q", fe.Error(), want)
			}
		})
	}
}

func TestDecodeJSONBodyRefusesUnknownFields(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"top level", `{` + head + `,"orcidId":"x"}`, "orcidId"},
		{"nested", `{` + head + `,"inner":{"year":2020,"month":1}}`, "inner.month"},
		{"before a missing field", `{"orcidId":"x"}`, "orcidId"},
		{"letter case counts", `{` + head + `,"Name":"x"}`, "Name"},
		{"the same one each time", `{` + head + `,"z":1,"b":2,"m":3}`, "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got body
			var unknown *UnknownFieldError
			if err := decodeJSONBody(strings.NewReader(tt.in), &got); !errors.As(err, &unknown) || unknown.Field != tt.want {
				t.Errorf("err = %v, want unknown field %q", err, tt.want)
			}
		})
	}
}

func TestDecodeJSONBodyWholeBody(t *testing.T) {
	tests := []struct {
		name, in string
		want     error
	}{
		{"not JSON", `nonsense`, ErrMalformedBody},
		{"cut off", `{`, ErrMalformedBody},
		{"cut off in a value", `{"name":"Ad`, ErrMalformedBody},
		{"trailing comma", `{"name":"Ada",}`, ErrMalformedBody},
		{"more after the value", `{} {}`, ErrMalformedBody},
		{"text after the value", `{} x`, ErrMalformedBody},
		{"empty", ``, io.EOF},
		{"only whitespace", "  \n", io.EOF},
		{"a list", `[]`, ErrBodyNotObject},
		{"a number", `5`, ErrBodyNotObject},
		{"null", `null`, ErrBodyNotObject},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got body
			if err := decodeJSONBody(strings.NewReader(tt.in), &got); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestDecodeJSONBodyPassesOnReadErrors(t *testing.T) {
	boom := errors.New("connection reset")
	var got body
	if err := decodeJSONBody(failingReader{boom}, &got); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

// A submitted value reaches neither the answer nor the logs, whichever way it fails.
func TestDecodeJSONBodyNeverRepeatsTheValue(t *testing.T) {
	const secret = "hunter2secret"
	for _, in := range []string{
		`{"id":"` + secret + `","name":"Ada","note":null}`,
		`{` + head + `,"age":"` + secret + `"}`,
		`{` + head + `,"tags":"` + secret + `"}`,
		`{` + head + `,"inner":"` + secret + `"}`,
		`{` + head + `,"headline":{"a":"` + secret + `"}}`,
		`{` + head + `,"limit":"` + secret + `"}`,
		`{"name":"` + secret,
		secret,
		`"` + secret + `"`,
		`{` + head + `} ` + secret,
	} {
		var got body
		err := decodeJSONBody(strings.NewReader(in), &got)
		if err == nil {
			t.Errorf("%s: no error", in)
			continue
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(fmt.Sprintf("%#v", err), secret) {
			t.Errorf("%s: error repeats the value: %v", in, err)
		}
	}
}

// `format: byte` fields are base64 text, required or nullable (slice 22, SyncFileStore).
func TestDecodeJSONBodyReadsBase64(t *testing.T) {
	type state struct {
		State []byte  `json:"state"`
		Maybe *[]byte `json:"maybe,omitempty"`
	}
	var got state
	if err := decodeJSONBody(strings.NewReader(`{"state":"AQID","maybe":"BA=="}`), &got); err != nil {
		t.Fatal(err)
	}
	if string(got.State) != "\x01\x02\x03" || got.Maybe == nil || string(*got.Maybe) != "\x04" {
		t.Errorf("got %v %v", got.State, got.Maybe)
	}
	for _, in := range []string{`{"state":[1,2]}`, `{"state":"not base64!"}`, `{"state":5}`} {
		var fe *FieldError
		if err := decodeJSONBody(strings.NewReader(in), &state{}); !errors.As(err, &fe) || fe.Field != "state" || !strings.Contains(fe.Error(), "base64 text") {
			t.Errorf("%s: %v, want a field error about base64 text", in, err)
		}
	}
}
