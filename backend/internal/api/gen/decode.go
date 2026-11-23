package gen

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Errors of a JSON request body that no one field is at fault for. They carry nothing of
// the body: encoding/json's syntax errors quote it, and these reach the logs.
var (
	ErrMalformedBody = errors.New("request body is not valid JSON")
	ErrBodyNotObject = errors.New("request body is not a JSON object")
)

// UnknownFieldError reports a JSON request body field that the contract does not define.
// The generated handlers pass it, wrapped, to RequestErrorHandlerFunc.
type UnknownFieldError struct {
	Field string
}

func (e *UnknownFieldError) Error() string {
	return fmt.Sprintf("unknown field %q", e.Field)
}

// FieldError reports a body field or parameter that breaks the contract. Field is its
// name, a dotted path with list indexes for a nested one. Its text is a sentence for the
// client, and never holds the value that was sent.
type FieldError struct {
	Field   string
	Problem string // such as "must be a UUID" or "is required"
}

func (e *FieldError) Error() string {
	return e.Field + " " + e.Problem + "."
}

// decodeJSONBody decodes a JSON request body into v and says which field is wrong, and
// why, in a *FieldError. encoding/json cannot: it does not notice a missing field, and an
// error from a UUID's parser does not name the field. So each field is decoded on its own,
// walking the generated struct. A field with no omitempty in its tag
// is required, also when it is nullable; fields v does not have are refused, so a client
// mistake is not silently ignored, and names match exactly.
//
// Every generated handler reads its body through it (templates/strict-http.tmpl). An empty
// body returns io.EOF, and read errors are returned unchanged.
func decodeJSONBody(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) || errors.Is(err, io.ErrUnexpectedEOF) {
			return ErrMalformedBody
		}
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ErrMalformedBody // something follows the JSON value
	}
	return decodeValue("", raw, reflect.ValueOf(v).Elem())
}

var (
	unmarshalerType     = reflect.TypeFor[json.Unmarshaler]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// decodesItself reports whether a value of type t has its own JSON decoding, as UUIDs,
// times and nullable.Nullable do.
func decodesItself(t reflect.Type) bool {
	p := reflect.PointerTo(t)
	return p.Implements(unmarshalerType) || p.Implements(textUnmarshalerType)
}

// bytesType is a `format: byte` field.
var bytesType = reflect.TypeFor[[]byte]()

// isNullable reports whether t is a nullable.Nullable, a map with its own decoding.
func isNullable(t reflect.Type) bool {
	return t.Kind() == reflect.Map && decodesItself(t)
}

func decodeValue(path string, raw json.RawMessage, v reflect.Value) error {
	t := v.Type()
	isNull := string(raw) == "null"
	switch {
	case t.Kind() == reflect.Pointer:
		if isNull {
			return nil
		}
		v.Set(reflect.New(t.Elem()))
		return decodeValue(path, raw, v.Elem())
	case isNull && !isNullable(t):
		return wrongValue(path, t) // encoding/json would leave the zero value
	case decodesItself(t):
		// The error is dropped: parsers quote what they could not read.
		if err := json.Unmarshal(raw, v.Addr().Interface()); err != nil {
			return wrongValue(path, t)
		}
		return nil
	case t.Kind() == reflect.Struct:
		return decodeStruct(path, raw, v)
	case t == bytesType:
		// `format: byte`: base64 text only (encoding/json would also take a list of numbers).
		if len(raw) == 0 || raw[0] != '"' {
			return wrongValue(path, t)
		}
		if err := json.Unmarshal(raw, v.Addr().Interface()); err != nil {
			return wrongValue(path, t)
		}
		return nil
	case t.Kind() == reflect.Slice:
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return wrongValue(path, t)
		}
		v.Set(reflect.MakeSlice(t, len(items), len(items)))
		for i, item := range items {
			if err := decodeValue(fmt.Sprintf("%s[%d]", path, i), item, v.Index(i)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := json.Unmarshal(raw, v.Addr().Interface()); err != nil {
		return wrongValue(path, t)
	}
	return nil
}

// decodeStruct fills a struct from a JSON object: unknown fields first, then the struct's
// fields in order, so the same mistakes always give the same answer.
func decodeStruct(path string, raw json.RawMessage, v reflect.Value) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return wrongValue(path, v.Type())
	}
	t := v.Type()
	known := make(map[string]bool, t.NumField())
	for f := range t.Fields() {
		if name, _ := fieldName(f); name != "" {
			known[name] = true
		}
	}
	var unknown []string
	for name := range object {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return &UnknownFieldError{Field: join(path, slices.Min(unknown))}
	}
	for i, f := range slices.Collect(t.Fields()) {
		name, required := fieldName(f)
		if name == "" {
			continue
		}
		value, present := object[name]
		switch {
		case present:
			if err := decodeValue(join(path, name), value, v.Field(i)); err != nil {
				return err
			}
		case required:
			return &FieldError{Field: join(path, name), Problem: "is required"}
		}
	}
	return nil
}

// fieldName returns a struct field's JSON name, empty when it has none, and whether the
// contract requires it: oapi-codegen writes omitempty on every optional field.
func fieldName(f reflect.StructField) (string, bool) {
	tag := f.Tag.Get("json")
	if !f.IsExported() || tag == "" || tag == "-" {
		return "", false
	}
	name, options, _ := strings.Cut(tag, ",")
	return name, !slices.Contains(strings.Split(options, ","), "omitempty")
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// wrongValue describes a value that is not what type t needs, from the type alone.
func wrongValue(path string, t reflect.Type) error {
	if path == "" {
		return ErrBodyNotObject
	}
	return &FieldError{Field: path, Problem: "must be " + expected(t)}
}

func expected(t reflect.Type) string {
	switch t {
	case reflect.TypeFor[openapi_types.UUID]():
		return "a UUID"
	case reflect.TypeFor[time.Time]():
		return "a date and time"
	case reflect.TypeFor[openapi_types.Date]():
		return "a date"
	case bytesType:
		return "base64 text"
	}
	if isNullable(t) {
		return expected(t.Elem())
	}
	switch t.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "true or false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "a whole number"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Slice, reflect.Array:
		return "a list"
	case reflect.Struct, reflect.Map:
		return "an object"
	}
	return "valid"
}
