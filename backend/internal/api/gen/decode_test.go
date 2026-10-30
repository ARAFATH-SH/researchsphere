package gen

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestDecodeJSONBody(t *testing.T) {
	type body struct {
		Name  string `json:"name"`
		Inner *struct {
			Year int `json:"year"`
		} `json:"inner"`
	}
	tests := []struct {
		name        string
		in          string
		wantUnknown string // field named by the UnknownFieldError, if one is wanted
		wantEOF     bool
		wantErr     bool
	}{
		{name: "known fields", in: `{"name":"Ada","inner":{"year":2020}}`},
		{name: "unknown top-level field", in: `{"name":"Ada","orcidId":"x"}`, wantUnknown: "orcidId", wantErr: true},
		{name: "unknown nested field", in: `{"inner":{"year":2020,"month":1}}`, wantUnknown: "month", wantErr: true},
		{name: "not JSON", in: `{`, wantErr: true},
		{name: "empty body", in: ``, wantEOF: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got body
			err := decodeJSONBody(strings.NewReader(tt.in), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			// The generated handlers wrap the error before passing it on.
			wrapped := fmt.Errorf("can't decode JSON body: %w", err)
			var unknown *UnknownFieldError
			isUnknown := errors.As(wrapped, &unknown)
			if isUnknown != (tt.wantUnknown != "") {
				t.Fatalf("UnknownFieldError = %v, want one: %v (err %v)", isUnknown, tt.wantUnknown != "", err)
			}
			if isUnknown && unknown.Field != tt.wantUnknown {
				t.Errorf("Field = %q, want %q", unknown.Field, tt.wantUnknown)
			}
			if errors.Is(wrapped, io.EOF) != tt.wantEOF {
				t.Errorf("errors.Is(io.EOF) = %v, want %v", !tt.wantEOF, tt.wantEOF)
			}
		})
	}
}
