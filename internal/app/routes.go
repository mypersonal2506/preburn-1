package app

import (
	"io"
	"net/http"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/customers"
	"github.com/preburn/preburn/internal/customerstate"
	"github.com/preburn/preburn/internal/dashboard"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/installation"
	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/pricing"
	"github.com/preburn/preburn/internal/revenue"
	"github.com/preburn/preburn/internal/version"
)

// RegisterRoutes adds every domain's routes to api, with the domain services
// of application. The admin customer routes and the dashboard read routes
// get services created here, because they hold no state of their own.
// WriteOpenAPI passes an App with zero values, so registration stores the
// services and never uses them.
func RegisterRoutes(api *httpapi.API, application *App) {
	states := customerstate.NewLoader(application.Pool)
	installation.RegisterRoutes(api, application.Installation)
	installation.RegisterSettingsRoutes(api, application.Settings)
	members.RegisterRoutes(api, application.Members)
	members.RegisterManagementRoutes(api, application.Members)
	apikeys.RegisterRoutes(api, application.APIKeys)
	customers.RegisterRoutes(api, application.Customers)
	customers.RegisterAdminRoutes(api, application.Customers, states, application.Counters)
	plans.RegisterRoutes(api, application.Plans)
	pricing.RegisterRoutes(api, application.Pricing)
	pricing.RegisterUncostedRoutes(api, application.Pricing)
	policies.RegisterRoutes(api, application.Policies)
	revenue.RegisterRoutes(api, application.Revenue)
	decisions.RegisterCheckRoutes(api, application.Checks)
	decisions.RegisterReportRoutes(api, application.Reports)
	dashboard.RegisterOverviewRoutes(api, dashboard.NewOverviewService(application.Pool, application.Cache, states, application.Counters, application.Clock))
	dashboard.RegisterCustomerRoutes(api, dashboard.NewCustomerService(application.Pool, states, application.Counters, application.Clock))
	dashboard.RegisterDecisionRoutes(api, dashboard.NewDecisionService(application.Pool))
	dashboard.RegisterStreamRoutes(api, application.DecisionStream)
	dashboard.RegisterEventRoutes(api, dashboard.NewEventService(application.Pool))
	dashboard.RegisterOnboardingRoutes(api, dashboard.NewOnboardingService(application.Pool))
	dashboard.RegisterFeatureRoutes(api, dashboard.NewFeatureService(application.Pool, application.Clock))
}

// WriteOpenAPI writes the OpenAPI document of every route RegisterRoutes adds
// to writer, the output of preburn openapi. It needs no configuration and
// opens no connections.
func WriteOpenAPI(writer io.Writer) error {
	api := httpapi.NewAPI(http.NewServeMux(), version.Version, nil, httpapi.RejectingAuthenticator{})
	RegisterRoutes(api, &App{})
	return httpapi.WriteOpenAPI(api, writer)
}

func newAPI(mux *http.ServeMux, application *App) *httpapi.API {
	api := httpapi.NewAPI(mux, version.Version, application.Logger, newRequestAuthenticator(application))
	RegisterRoutes(api, application)
	return api
}
