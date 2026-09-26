package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// EnvironmentHeader is the request header that names the environment of a
// member session request, test or live.
const EnvironmentHeader = "X-Preburn-Environment"

// Environment is the environment a request acts in. Its values are the
// values of the Postgres enum environment.
type Environment string

const (
	// EnvironmentTest is the environment for integration and demo data.
	EnvironmentTest Environment = "test"
	// EnvironmentLive is the environment for production traffic.
	EnvironmentLive Environment = "live"
)

// EnvironmentPrincipal is a Principal that acts in one environment. An API
// key principal takes it from its key and MemberPrincipal from the
// X-Preburn-Environment header, and handlers on the admin and dashboard groups
// read it the same way for both with EnvironmentFromContext.
type EnvironmentPrincipal interface {
	// PrincipalEnvironment returns the environment the request acts in.
	PrincipalEnvironment() Environment
}

// MemberPrincipal is the Principal of a request authenticated by a member
// session.
type MemberPrincipal struct {
	// MemberID is the id of the member who owns the session.
	MemberID uuid.UUID
	// Environment is the environment the X-Preburn-Environment header names.
	Environment Environment
}

// ErrUnknownEnvironment reports a value that ParseEnvironment rejects.
var ErrUnknownEnvironment = errors.New("environment must be test or live")

// ErrEnvironmentHeaderInvalid is the CodedError for a member session request
// whose X-Preburn-Environment header is missing, repeated or names no
// environment: 422 environment_header_invalid.
var ErrEnvironmentHeaderInvalid error = &codedError{
	status:  http.StatusUnprocessableEntity,
	code:    codeEnvironmentHeaderInvalid,
	message: "X-Preburn-Environment must be test or live",
}

// ParseEnvironment returns the Environment named exactly value, test or
// live. Any other value, such as an empty string or LIVE, returns
// ErrUnknownEnvironment.
func ParseEnvironment(value string) (Environment, error) {
	environment := Environment(value)
	if environment != EnvironmentTest && environment != EnvironmentLive {
		return "", ErrUnknownEnvironment
	}
	return environment, nil
}

// EnvironmentFromHeader returns the Environment that the X-Preburn-Environment
// field of header names. It returns ErrEnvironmentHeaderInvalid unless the
// field appears exactly once and ParseEnvironment accepts its value.
func EnvironmentFromHeader(header http.Header) (Environment, error) {
	values := header.Values(EnvironmentHeader)
	if len(values) != 1 {
		return "", ErrEnvironmentHeaderInvalid
	}
	environment, err := ParseEnvironment(values[0])
	if err != nil {
		return "", ErrEnvironmentHeaderInvalid
	}
	return environment, nil
}

// PrincipalEnvironment returns the environment the X-Preburn-Environment
// header of the request named.
func (principal MemberPrincipal) PrincipalEnvironment() Environment {
	return principal.Environment
}

// EnvironmentFromContext returns the environment of the principal of the
// request of ctx. It returns an error when the principal is not an
// EnvironmentPrincipal, as on a route of RouteGroupPublic, which the handler
// returns so the request ends with a 500 internal_error.
func EnvironmentFromContext(ctx context.Context) (Environment, error) {
	principal := PrincipalFromContext(ctx)
	environmentPrincipal, isEnvironmentPrincipal := principal.(EnvironmentPrincipal)
	if !isEnvironmentPrincipal {
		return "", fmt.Errorf("principal %T has no environment", principal)
	}
	return environmentPrincipal.PrincipalEnvironment(), nil
}

// MemberPrincipalFromContext returns the MemberPrincipal of the request of
// ctx. It returns an error when a member session did not authenticate the
// request, which on a route of RouteGroupDashboard never happens, and the
// handler returns it so the request ends with a 500 internal_error.
func MemberPrincipalFromContext(ctx context.Context) (MemberPrincipal, error) {
	principal := PrincipalFromContext(ctx)
	memberPrincipal, isMemberPrincipal := principal.(MemberPrincipal)
	if !isMemberPrincipal {
		return MemberPrincipal{}, fmt.Errorf("principal %T is not a member session", principal)
	}
	return memberPrincipal, nil
}
