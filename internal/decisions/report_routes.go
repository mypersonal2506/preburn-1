package decisions

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
)

type reportRoutes struct {
	service *ReportService
}

type reportInput struct {
	DroppedReports int64 `header:"Preburn-Dropped-Reports" minimum:"0" doc:"Reports the SDK dropped since it last sent this header, added to the day's dropped report count when the request answers 202."`
	Body           ReportRequest
}

type reportOutput struct {
	Body ReportResult
}

type reportBatchInput struct {
	DroppedReports int64 `header:"Preburn-Dropped-Reports" minimum:"0" doc:"Reports the SDK dropped since it last sent this header, added to the day's dropped report count when the request answers 202."`
	Body           reportBatchRequest
}

type reportBatchRequest struct {
	Reports []ReportRequest `json:"reports" nullable:"false" doc:"At most 500 reports. Each one is stored or rejected on its own."`
}

type reportBatchOutput struct {
	Body reportBatchResponse
}

type reportBatchResponse struct {
	Results []ReportBatchResult `json:"results" nullable:"false" doc:"One result per report, at the index of the report."`
}

type releaseInput struct {
	Body releaseRequest
}

type releaseRequest struct {
	DecisionID string `json:"decision_id" doc:"Decision whose reservation to free, such as dec_01jbvagescfn78y0938nkrkayd."`
}

type releaseOutput struct {
	Body ReleaseResult
}

// RegisterReportRoutes adds POST /api/v1/report, POST /api/v1/reports and
// POST /api/v1/release of service to api on the runtime group. They act in
// the environment of the API key. Handlers read service only when they run.
func RegisterReportRoutes(api *httpapi.API, service *ReportService) {
	routes := &reportRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupRuntime, huma.Operation{
		OperationID:   "report-usage",
		Method:        http.MethodPost,
		Path:          "/api/v1/report",
		Summary:       "Report usage",
		Description:   "Stores the usage of a request as a ledger entry with its cost and moves it into the customer's period counter. A server report names the decision of its check and settles its reservation. A fallback report names the customer, feature, provider and model of a request that ran without a decision. Reporting the same decision or idempotency key again returns the first entry with duplicate true. Returns 409 decision_not_reportable for a denied decision.",
		Tags:          []string{decisionsTag},
		DefaultStatus: http.StatusAccepted,
	}, routes.report)
	httpapi.Register(api, httpapi.RouteGroupRuntime, huma.Operation{
		OperationID:   "report-usage-batch",
		Method:        http.MethodPost,
		Path:          "/api/v1/reports",
		Summary:       "Report usage in a batch",
		Description:   "Stores up to 500 reports, each as POST /api/v1/report would, and returns one result per report. A failed report does not stop the others. More than 500 reports return 422 and store nothing.",
		Tags:          []string{decisionsTag},
		DefaultStatus: http.StatusAccepted,
	}, routes.reportBatch)
	httpapi.Register(api, httpapi.RouteGroupRuntime, huma.Operation{
		OperationID: "release-decision",
		Method:      http.MethodPost,
		Path:        "/api/v1/release",
		Summary:     "Release a decision",
		Description: "Frees the reservation of a decision whose request did not run. Releasing it again, or releasing a decision that was reported, expired or denied, changes nothing and returns its status.",
		Tags:        []string{decisionsTag},
	}, routes.release)
}

func (routes *reportRoutes) report(ctx context.Context, input *reportInput) (*reportOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	result, err := routes.service.Report(ctx, environment, input.Body)
	if err != nil {
		return nil, err
	}
	routes.service.RecordDroppedReports(ctx, environment, input.DroppedReports)
	return &reportOutput{Body: result}, nil
}

func (routes *reportRoutes) reportBatch(ctx context.Context, input *reportBatchInput) (*reportBatchOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	results, err := routes.service.ReportBatch(ctx, environment, input.Body.Reports)
	if err != nil {
		return nil, err
	}
	routes.service.RecordDroppedReports(ctx, environment, input.DroppedReports)
	return &reportBatchOutput{Body: reportBatchResponse{Results: results}}, nil
}

func (routes *reportRoutes) release(ctx context.Context, input *releaseInput) (*releaseOutput, error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	decisionID, err := identifiers.Decode(identifiers.PrefixDecision, input.Body.DecisionID)
	if err != nil {
		return nil, httpapi.NewValidationProblem(httpapi.ProblemError{Location: decisionIDLocation, Message: decisionIDRule})
	}
	result, err := routes.service.Release(ctx, environment, decisionID)
	if err != nil {
		return nil, err
	}
	return &releaseOutput{Body: result}, nil
}
