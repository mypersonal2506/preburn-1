package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
)

const (
	// ListLimitDefault is the page size of a list request without a limit.
	ListLimitDefault = 50
	// ListLimitMaximum is the largest page size a list request may ask for.
	// ParseLimit rejects a larger limit.
	ListLimitMaximum = 100

	pageBodySchemaPrefix = "PageBody"
)

// Cursor encodes and decodes the cursors of one listing. A cursor is opaque
// base64url JSON holding the listing name, the environment of the request
// that listed the page and the sort key of the last item of the page, of type
// Key. Create one with NewCursor.
type Cursor[Key any] struct {
	listing string
}

type cursorPayload[Key any] struct {
	Listing     string      `json:"listing"`
	Environment Environment `json:"environment"`
	Key         Key         `json:"key"`
}

// Page is the output of a list operation: a page of items and the cursor of
// the next page. Create one with NewPage.
type Page[Item any] struct {
	Body PageBody[Item]
}

// PageBody is the response body of a list operation.
type PageBody[Item any] struct {
	Items      []Item  `json:"items" nullable:"false" doc:"Items of this page, in list order."`
	NextCursor *string `json:"next_cursor" doc:"Cursor of the next page, null on the last page."`
}

// ErrInvalidCursor is the CodedError for a cursor that is malformed or comes
// from another listing or environment: 422 invalid_cursor.
var ErrInvalidCursor error = &codedError{
	status:  http.StatusUnprocessableEntity,
	code:    codeInvalidCursor,
	message: "invalid cursor",
}

// NewCursor returns the Cursor of the listing named listing, such as
// decisions.
func NewCursor[Key any](listing string) Cursor[Key] {
	return Cursor[Key]{listing: listing}
}

// Encode returns the cursor that resumes the listing in environment after
// key.
func (cursor Cursor[Key]) Encode(environment Environment, key Key) (string, error) {
	payload, err := json.Marshal(cursorPayload[Key]{Listing: cursor.listing, Environment: environment, Key: key})
	if err != nil {
		return "", fmt.Errorf("encode cursor listing=%s: %w", cursor.listing, err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// Decode returns the sort key held by encoded. An encoded value that is not a
// cursor of this listing in environment returns an error wrapping
// ErrInvalidCursor.
func (cursor Cursor[Key]) Decode(environment Environment, encoded string) (Key, error) {
	var payload cursorPayload[Key]
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return payload.Key, fmt.Errorf("%w: %w", ErrInvalidCursor, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return payload.Key, fmt.Errorf("%w: %w", ErrInvalidCursor, err)
	}
	if payload.Listing != cursor.listing {
		return payload.Key, fmt.Errorf("%w: listing=%s want=%s", ErrInvalidCursor, payload.Listing, cursor.listing)
	}
	if payload.Environment != environment {
		return payload.Key, fmt.Errorf("%w: listing=%s environment=%s want=%s", ErrInvalidCursor, cursor.listing, payload.Environment, environment)
	}
	return payload.Key, nil
}

// ParseLimit returns the page size for the limit query parameter. A limit of
// 0, which an absent parameter leaves, selects ListLimitDefault. A limit
// below 1 or above ListLimitMaximum returns a 422 validation_failed problem
// located at query.limit.
func ParseLimit(limit int) (int, error) {
	if limit == 0 {
		return ListLimitDefault, nil
	}
	if limit < 1 || limit > ListLimitMaximum {
		return 0, validationProblem("query.limit", fmt.Sprintf("expected limit from 1 to %d", ListLimitMaximum))
	}
	return limit, nil
}

// SchemaName returns PageBody followed by the schema name of Item, such as
// PageBodyPlanResponse, so a page follows the NamedSchema of its items.
func (PageBody[Item]) SchemaName() string {
	return pageBodySchemaPrefix + schemaName(reflect.TypeFor[Item](), "")
}

// NewPage returns the page holding items, with an empty items array when
// items is nil. An empty nextCursor marks the last page and is sent as null.
func NewPage[Item any](items []Item, nextCursor string) *Page[Item] {
	page := &Page[Item]{Body: PageBody[Item]{Items: items}}
	if items == nil {
		page.Body.Items = []Item{}
	}
	if nextCursor != "" {
		page.Body.NextCursor = &nextCursor
	}
	return page
}
