package gen

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// UnknownFieldError reports a JSON request body field that the contract does not define.
// The generated handlers pass it, wrapped, to RequestErrorHandlerFunc.
type UnknownFieldError struct {
	Field string
}

func (e *UnknownFieldError) Error() string {
	return fmt.Sprintf("unknown field %q", e.Field)
}

// decodeJSONBody decodes a JSON request body into v and refuses fields that v does not
// have, so a client mistake is not silently ignored. Every generated handler reads its
// body through it (templates/strict-http.tmpl). Other errors, io.EOF for an empty body
// included, are returned unchanged.
func decodeJSONBody(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	// encoding/json has no error type for this case, only the message.
	if err != nil {
		if rest, ok := strings.CutPrefix(err.Error(), `json: unknown field "`); ok {
			return &UnknownFieldError{Field: strings.TrimSuffix(rest, `"`)}
		}
	}
	return err
}
