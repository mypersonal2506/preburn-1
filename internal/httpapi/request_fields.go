package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	bodyFieldName        = "Body"
	jsonTagName          = "json"
	skippedJSONFieldName = "-"
	storableTextRule     = "expected valid Unicode text without NUL characters"
	unicodeEscapeDigits  = 4
	lowSurrogateFirst    = 0xDC00
	lowSurrogateLast     = 0xDFFF
)

type textCheck struct {
	problems []ProblemError
	err      error
}

var (
	parameterLocations  = []string{"path", "query"}
	unicodeEscapePrefix = []byte(`\u`)
	jsonMarshalerType   = reflect.TypeFor[json.Marshaler]()
	jsonUnmarshalerType = reflect.TypeFor[json.Unmarshaler]()
)

// requireStorableText returns the validation problem that names every path
// parameter, query parameter and body field of input, a pointer to a Huma
// input struct, holding text Postgres cannot store: invalid UTF-8, a NUL
// character, or in raw JSON a NUL escape or an unpaired surrogate escape. A
// map field is named as a whole, so the location never repeats a key the
// request sent. It returns nil when every text is storable.
func requireStorableText(input any) error {
	check := &textCheck{}
	check.parameters(reflect.ValueOf(input).Elem())
	if check.err != nil {
		return check.err
	}
	if len(check.problems) > 0 {
		return NewValidationProblem(check.problems...)
	}
	return nil
}

// declaredLocation returns location, a validation location Huma built for a
// request of inputType, cut after the first map field of the request body on
// its path, or after a field whose type decodes its own JSON, so the
// location never repeats a map key the request sent. Locations outside the
// body and paths that leave the declared fields stay as they are.
func declaredLocation(inputType reflect.Type, location string) string {
	path, inBody := strings.CutPrefix(location, bodyLocation)
	body, hasBody := inputType.FieldByName(bodyFieldName)
	if !inBody || !hasBody {
		return location
	}
	return bodyLocation + mapFieldPath(body.Type, path)
}

func (check *textCheck) parameters(input reflect.Value) {
	for index := range input.NumField() {
		field := input.Type().Field(index)
		value := input.Field(index)
		switch {
		case !field.IsExported():
		case field.Name == bodyFieldName:
			check.value(bodyLocation, value)
		case field.Anonymous && field.Type.Kind() == reflect.Struct:
			check.parameters(value)
		default:
			for _, kind := range parameterLocations {
				if name, tagged := field.Tag.Lookup(kind); tagged {
					check.value(kind+"."+name, value)
				}
			}
		}
	}
}

func (check *textCheck) value(location string, value reflect.Value) {
	switch {
	case value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface:
		if !value.IsNil() {
			check.value(location, value.Elem())
		}
	case value.Kind() == reflect.Map || value.Type().Implements(jsonMarshalerType):
		check.marshaled(location, value)
	case value.Kind() == reflect.String:
		if !storableText(value.String()) {
			check.add(location)
		}
	case value.Kind() == reflect.Struct:
		check.fields(location, value, decodesItself(value.Type()))
	case (value.Kind() == reflect.Slice || value.Kind() == reflect.Array) && value.Type().Elem().Kind() != reflect.Uint8:
		for index := range value.Len() {
			check.value(location+"["+strconv.Itoa(index)+"]", value.Index(index))
		}
	}
}

func (check *textCheck) fields(location string, value reflect.Value, sameLocation bool) {
	for index := range value.NumField() {
		field := value.Type().Field(index)
		name, named := jsonFieldName(field)
		switch {
		case !field.IsExported() || !named:
		case sameLocation || (field.Anonymous && name == field.Name && field.Type.Kind() == reflect.Struct):
			check.value(location, value.Field(index))
		default:
			check.value(location+"."+name, value.Field(index))
		}
	}
}

func (check *textCheck) marshaled(location string, value reflect.Value) {
	encoded, err := json.Marshal(value.Interface())
	if err != nil {
		check.err = errors.Join(check.err, fmt.Errorf("encode %s: %w", location, err))
		return
	}
	if !storableJSON(encoded) {
		check.add(location)
	}
}

func (check *textCheck) add(location string) {
	if !slices.ContainsFunc(check.problems, func(problem ProblemError) bool { return problem.Location == location }) {
		check.problems = append(check.problems, ProblemError{Location: location, Message: storableTextRule})
	}
}

func mapFieldPath(fieldType reflect.Type, path string) string {
	consumed := 0
	for consumed < len(path) {
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		remaining := path[consumed:]
		switch {
		case fieldType.Kind() == reflect.Map || decodesItself(fieldType):
			return path[:consumed]
		case fieldType.Kind() == reflect.Struct && strings.HasPrefix(remaining, "."):
			name, _, _ := strings.Cut(remaining[1:], ".")
			name, _, _ = strings.Cut(name, "[")
			field, found := jsonField(fieldType, name)
			if !found {
				return path
			}
			fieldType = field.Type
			consumed += len(".") + len(name)
		case (fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array) && strings.HasPrefix(remaining, "["):
			end := strings.IndexByte(remaining, ']')
			if end < 0 {
				return path
			}
			fieldType = fieldType.Elem()
			consumed += end + len("]")
		default:
			return path
		}
	}
	return path
}

func jsonField(structType reflect.Type, name string) (reflect.StructField, bool) {
	for index := range structType.NumField() {
		field := structType.Field(index)
		fieldName, named := jsonFieldName(field)
		if !field.IsExported() || !named {
			continue
		}
		if field.Anonymous && fieldName == field.Name && field.Type.Kind() == reflect.Struct {
			if embedded, found := jsonField(field.Type, name); found {
				return embedded, true
			}
			continue
		}
		if fieldName == name {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

func jsonFieldName(field reflect.StructField) (string, bool) {
	name, _, _ := strings.Cut(field.Tag.Get(jsonTagName), ",")
	switch name {
	case skippedJSONFieldName:
		return "", false
	case "":
		return field.Name, true
	}
	return name, true
}

func decodesItself(valueType reflect.Type) bool {
	return reflect.PointerTo(valueType).Implements(jsonUnmarshalerType)
}

func storableText(text string) bool {
	return utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}

func storableJSON(text []byte) bool {
	if !utf8.Valid(text) {
		return false
	}
	for index := 0; index < len(text); index++ {
		if text[index] != '\\' {
			continue
		}
		index++
		if text[index] != 'u' {
			continue
		}
		unit, valid := escapedUnit(text[index+1:])
		index += unicodeEscapeDigits
		if !valid || unit == 0 || isLowSurrogate(unit) {
			return false
		}
		if !utf16.IsSurrogate(unit) {
			continue
		}
		next, escaped := bytes.CutPrefix(text[index+1:], unicodeEscapePrefix)
		if !escaped {
			return false
		}
		low, valid := escapedUnit(next)
		if !valid || !isLowSurrogate(low) {
			return false
		}
		index += len(unicodeEscapePrefix) + unicodeEscapeDigits
	}
	return true
}

func escapedUnit(escape []byte) (rune, bool) {
	unit, err := strconv.ParseUint(string(escape[:unicodeEscapeDigits]), 16, 16)
	return rune(unit), err == nil
}

func isLowSurrogate(unit rune) bool {
	return unit >= lowSurrogateFirst && unit <= lowSurrogateLast
}
