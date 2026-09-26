package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/httpapi"
)

const echoedKey = "<b>secret-value</b>"

type mappedThingInput struct {
	Body mappedThingBody
}

type mappedThingBody struct {
	HoldTimes map[string]int         `json:"hold_times,omitempty"`
	Usage     map[string]string      `json:"usage,omitempty"`
	Groups    []mappedThingGroup     `json:"groups,omitempty"`
	Owner     *mappedThingOwner      `json:"owner,omitempty"`
	Nested    map[string]map[int]int `json:"nested,omitempty"`
}

type mappedThingGroup struct {
	Limits map[string]int `json:"limits"`
}

type mappedThingOwner struct {
	Name string `json:"name" maxLength:"3"`
}

func TestValidationLocationsStopAtMapFields(t *testing.T) {
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "create-mapped-thing",
		Method:      http.MethodPost,
		Path:        "/api/v1/mapped-things",
	}, func(context.Context, *mappedThingInput) (*thingOutput, error) {
		t.Error("handler ran for an invalid body")
		return &thingOutput{}, nil
	})
	tests := []struct {
		name      string
		body      string
		locations []string
	}{
		{name: "map value of the wrong type", body: `{"hold_times":{"` + echoedKey + `":"a"}}`, locations: []string{"body.hold_times"}},
		{name: "string map value that is a number", body: `{"usage":{"` + echoedKey + `":8}}`, locations: []string{"body.usage"}},
		{name: "map inside a list", body: `{"groups":[{"limits":{}},{"limits":{"` + echoedKey + `":"a"}}]}`, locations: []string{"body.groups[1].limits"}},
		{name: "map key the schema allows and decoding rejects", body: `{"nested":{"` + echoedKey + `":{"a":1}}}`, locations: []string{"body"}},
		{name: "map value the schema allows and decoding rejects", body: `{"hold_times":{"` + echoedKey + `":1e30}}`, locations: []string{"body"}},
		{name: "field of a struct", body: `{"owner":{"name":"long name"}}`, locations: []string{"body.owner.name"}},
		{name: "map field itself", body: `{"hold_times":[1]}`, locations: []string{"body.hold_times"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := server.serve(newRequest(t, http.MethodPost, "/api/v1/mapped-things", test.body))

			problem := assertProblem(t, recorder, http.StatusUnprocessableEntity, "validation_failed")
			var locations []string
			for _, fieldError := range problem["errors"].([]any) {
				locations = append(locations, fieldError.(map[string]any)["location"].(string))
			}
			if diff := cmp.Diff(test.locations, locations); diff != "" {
				t.Errorf("locations mismatch (-want +got):\n%s", diff)
			}
			if strings.Contains(recorder.Body.String(), "secret-value") {
				t.Errorf("problem echoes a request key: %s", recorder.Body.String())
			}
		})
	}
}
