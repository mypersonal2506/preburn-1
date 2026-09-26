package revenue

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
)

const revenueTag = "Revenue"

// EntryResponse is the API representation of a revenue entry.
type EntryResponse struct {
	ID              string    `json:"id" doc:"Revenue entry id, such as rev_01jbvagescfn78y0938nkrkayd."`
	CustomerID      string    `json:"customer_id" doc:"Id of the customer in your system."`
	Kind            Kind      `json:"kind" enum:"subscription,adjustment,stripe_fee,refund,credit_note" doc:"subscription and adjustment add the amount to net revenue. stripe_fee, refund and credit_note subtract it."`
	Amount          string    `json:"amount" doc:"Non-negative amount in USD with 9 decimals. The kind sets its sign."`
	PeriodStart     time.Time `json:"period_start" doc:"Start of the period the entry belongs to."`
	PeriodEnd       time.Time `json:"period_end" doc:"End of the period. Equal to period_start for a one-time line."`
	Source          Source    `json:"source" enum:"api,stripe,import" doc:"Where the entry came from: the revenue API, the Stripe connector or a data import."`
	SourceReference string    `json:"source_reference" doc:"Id of the entry in its source, unique per environment, source and kind."`
	OccurredAt      time.Time `json:"occurred_at" doc:"When the revenue was recognized."`
	CreatedAt       time.Time `json:"created_at" doc:"When the entry was recorded."`
}

// RecordedEntryResponse is the response of POST /api/v1/revenue: the entry
// and whether an earlier request recorded it.
type RecordedEntryResponse struct {
	EntryResponse
	Duplicate bool `json:"duplicate" doc:"True when an earlier request recorded the entry and this one changed nothing."`
}

type revenueRoutes struct {
	service *Service
}

type recordRevenueInput struct {
	Body recordRevenueRequest
}

type recordRevenueRequest struct {
	CustomerID      string     `json:"customer_id" doc:"Id of the customer in your system: 1 to 128 letters, digits or the characters . _ : @ -. An unknown customer is created and follows the default plan."`
	Kind            Kind       `json:"kind" enum:"subscription,adjustment,stripe_fee,refund,credit_note" doc:"subscription and adjustment add the amount to net revenue. stripe_fee, refund and credit_note subtract it."`
	Amount          string     `json:"amount" doc:"Non-negative amount in USD with at most 9 decimals, such as 30.00. The kind sets its sign."`
	PeriodStart     time.Time  `json:"period_start" doc:"Start of the period the revenue belongs to. A subscription period becomes a billing period of the customer."`
	PeriodEnd       time.Time  `json:"period_end" doc:"End of the period, no earlier than period_start and at most 400 days after it. Equal to period_start for a one-time line."`
	SourceReference string     `json:"source_reference" doc:"Your id for the entry, 1 to 200 characters, such as an invoice line id. Recording the same kind and source reference again returns the first entry."`
	OccurredAt      *time.Time `json:"occurred_at,omitempty" doc:"When the revenue was recognized. The time of the request when omitted."`
}

type recordRevenueOutput struct {
	Status int
	Body   RecordedEntryResponse
}

type listRevenueInput struct {
	CustomerID string `query:"customer_id" doc:"Keeps the entries of the customer with this id in your system."`
	Kind       Kind   `query:"kind" enum:"subscription,adjustment,stripe_fee,refund,credit_note" doc:"Keeps the entries of this kind."`
	Cursor     string `query:"cursor" doc:"Cursor from next_cursor of the previous page. Omit it for the first page."`
	Limit      int    `query:"limit" doc:"Page size from 1 to 100, 50 when omitted."`
}

// RegisterRoutes adds the revenue routes of service to api: POST
// /api/v1/revenue on the runtime group and GET /api/v1/revenue on the admin
// group. They act in the environment of the API key or member session.
// Handlers read service only when they run.
func RegisterRoutes(api *httpapi.API, service *Service) {
	routes := &revenueRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupRuntime, huma.Operation{
		OperationID:   "record-revenue",
		Method:        http.MethodPost,
		Path:          "/api/v1/revenue",
		Summary:       "Record revenue",
		Description:   "Records a revenue entry and creates an unknown customer. The next check of the customer sees the new net revenue. Returns 201 with the new entry, or 200 with the first entry and duplicate true when the environment already has an entry of this kind and source reference from the revenue API.",
		Tags:          []string{revenueTag},
		DefaultStatus: http.StatusCreated,
		Responses: map[string]*huma.Response{
			strconv.Itoa(http.StatusOK): httpapi.JSONResponse[RecordedEntryResponse](api, "The environment already had an entry of this kind and source reference from the revenue API. The body is that entry with duplicate true."),
		},
	}, routes.record)
	httpapi.Register(api, httpapi.RouteGroupAdmin, huma.Operation{
		OperationID: "list-revenue",
		Method:      http.MethodGet,
		Path:        "/api/v1/revenue",
		Summary:     "List revenue entries",
		Description: "Returns the revenue entries of the environment from every source, newest first.",
		Tags:        []string{revenueTag},
	}, routes.list)
}

func (routes *revenueRoutes) record(ctx context.Context, input *recordRevenueInput) (*recordRevenueOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	amount, err := money.ParseAmount(input.Body.Amount)
	if err != nil {
		return nil, httpapi.NewValidationProblem(httpapi.ProblemError{Location: amountLocation, Message: amountFormatRule})
	}
	entry, duplicate, err := routes.service.Record(ctx, environment, SourceAPI, RecordInput{
		CustomerExternalID: input.Body.CustomerID,
		Kind:               input.Body.Kind,
		Amount:             amount,
		PeriodStart:        input.Body.PeriodStart,
		PeriodEnd:          input.Body.PeriodEnd,
		SourceReference:    input.Body.SourceReference,
		OccurredAt:         input.Body.OccurredAt,
	})
	if err != nil {
		return nil, err
	}
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	return &recordRevenueOutput{
		Status: status,
		Body:   RecordedEntryResponse{EntryResponse: newEntryResponse(entry), Duplicate: duplicate},
	}, nil
}

func (routes *revenueRoutes) list(ctx context.Context, input *listRevenueInput) (*httpapi.Page[EntryResponse], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	filter := ListFilter{CustomerExternalID: input.CustomerID, Kind: input.Kind}
	entries, nextCursor, err := routes.service.List(ctx, environment, filter, input.Cursor, input.Limit)
	if err != nil {
		return nil, err
	}
	responses := make([]EntryResponse, 0, len(entries))
	for _, entry := range entries {
		responses = append(responses, newEntryResponse(entry))
	}
	return httpapi.NewPage(responses, nextCursor), nil
}

// SchemaName returns RevenueEntryResponse, the name of the schema of
// EntryResponse in the OpenAPI document.
func (EntryResponse) SchemaName() string {
	return "RevenueEntryResponse"
}

// SchemaName returns RecordedRevenueEntryResponse, the name of the schema of
// RecordedEntryResponse in the OpenAPI document.
func (RecordedEntryResponse) SchemaName() string {
	return "RecordedRevenueEntryResponse"
}

func newEntryResponse(entry Entry) EntryResponse {
	return EntryResponse{
		ID:              identifiers.Encode(identifiers.PrefixRevenueEntry, entry.ID),
		CustomerID:      entry.CustomerExternalID,
		Kind:            entry.Kind,
		Amount:          money.FormatAmount(entry.Amount),
		PeriodStart:     entry.PeriodStart,
		PeriodEnd:       entry.PeriodEnd,
		Source:          entry.Source,
		SourceReference: entry.SourceReference,
		OccurredAt:      entry.OccurredAt,
		CreatedAt:       entry.CreatedAt,
	}
}
