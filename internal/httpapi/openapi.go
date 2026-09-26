package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const openAPIIndent = "  "

// WriteOpenAPI writes the OpenAPI document of api to writer as JSON with
// sorted keys, a 2-space indent and a final newline, so the same routes
// always produce the same bytes.
func WriteOpenAPI(api *API, writer io.Writer) error {
	document, err := json.Marshal(api.humaAPI.OpenAPI())
	if err != nil {
		return fmt.Errorf("marshal openapi document: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	var tree any
	if err := decoder.Decode(&tree); err != nil {
		return fmt.Errorf("decode openapi document: %w", err)
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", openAPIIndent)
	if err := encoder.Encode(tree); err != nil {
		return fmt.Errorf("write openapi document: %w", err)
	}
	return nil
}
