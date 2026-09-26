package members

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
)

const membersTag = "Members"

type managementRoutes struct {
	service *Service
}

type listMembersInput struct {
	Cursor string `query:"cursor" doc:"Cursor of the page to return, from next_cursor of the previous page."`
	Limit  int    `query:"limit" doc:"Number of members per page, from 1 to 100. Default 50."`
}

type addMemberInput struct {
	Body addMemberRequest
}

type addMemberRequest struct {
	Email       string `json:"email" doc:"Email address the new member signs in with."`
	DisplayName string `json:"display_name" doc:"Name the dashboard shows, 1 to 80 characters."`
}

type addMemberOutput struct {
	Body addedMemberResponse
}

type addedMemberResponse struct {
	Member  MemberResponse `json:"member" doc:"The new member, without a password until they accept the invite."`
	LinkURL string         `json:"link_url" doc:"Invite link that sets the member's password, valid for 24 hours. Only this response holds it."`
}

type memberPathInput struct {
	MemberID string `path:"member_id" doc:"Member id, such as mem_01jbvagescfn78y0938nkrkayd."`
}

type resetLinkOutput struct {
	Body resetLinkResponse
}

type resetLinkResponse struct {
	LinkURL string `json:"link_url" doc:"Password reset link, valid for 24 hours. Only this response holds it."`
}

type inspectLinkInput struct {
	Body inspectLinkRequest
}

type inspectLinkRequest struct {
	Token string `json:"token" doc:"Token from the fragment of the link URL."`
}

type linkDetailsOutput struct {
	Body linkDetailsResponse
}

type linkDetailsResponse struct {
	Purpose     LinkPurpose `json:"purpose" enum:"invite,password_reset" doc:"invite for a new member, password_reset for a password reset."`
	Email       string      `json:"email" doc:"Email address of the link's member."`
	DisplayName string      `json:"display_name" doc:"Display name of the link's member."`
}

type consumeLinkInput struct {
	httpapi.ClientDetails
	Body consumeLinkRequest
}

type consumeLinkRequest struct {
	Token    string `json:"token" doc:"Token from the fragment of the link URL."`
	Password string `json:"password" doc:"New password of the member, 12 to 256 characters."`
}

// RegisterManagementRoutes adds the member management routes of service to
// api: GET and POST /api/v1/members, DELETE /api/v1/members/{member_id} and
// POST /api/v1/members/{member_id}/reset-link on the dashboard group, and
// POST /api/v1/auth/links/inspect and POST /api/v1/auth/links/consume on the
// public group. Handlers read service only when they run.
func RegisterManagementRoutes(api *httpapi.API, service *Service) {
	routes := &managementRoutes{service: service}
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID: "list-members",
		Method:      http.MethodGet,
		Path:        "/api/v1/members",
		Summary:     "List members",
		Description: "Returns the members of the installation in the order they were added, removed members included.",
		Tags:        []string{membersTag},
	}, routes.listMembers)
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID:   "add-member",
		Method:        http.MethodPost,
		Path:          "/api/v1/members",
		Summary:       "Add a member",
		Description:   "Creates a member without a password and returns an invite link, valid for 24 hours, that sets the password.",
		Tags:          []string{membersTag},
		DefaultStatus: http.StatusCreated,
	}, routes.addMember)
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID:   "remove-member",
		Method:        http.MethodDelete,
		Path:          "/api/v1/members/{member_id}",
		Summary:       "Remove a member",
		Description:   "Disables the member and ends their sessions. Members cannot remove themselves or the last active member with a password.",
		Tags:          []string{membersTag},
		DefaultStatus: http.StatusNoContent,
	}, routes.removeMember)
	httpapi.Register(api, httpapi.RouteGroupDashboard, huma.Operation{
		OperationID:   "create-reset-link",
		Method:        http.MethodPost,
		Path:          "/api/v1/members/{member_id}/reset-link",
		Summary:       "Create a password reset link",
		Description:   "Returns a password reset link for the member, valid for 24 hours. Earlier unused invite and reset links of the member stop working.",
		Tags:          []string{membersTag},
		DefaultStatus: http.StatusCreated,
	}, routes.createResetLink)
	httpapi.Register(api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "inspect-link",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/links/inspect",
		Summary:     "Inspect a member link",
		Description: "Returns the purpose of an invite or password reset link and the member it belongs to.",
		Tags:        []string{authenticationTag},
	}, routes.inspectLink)
	httpapi.Register(api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "consume-link",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/links/consume",
		Summary:     "Use a member link",
		Description: "Sets the member's password, uses up the link and every other unused link of the member, ends the member's earlier sessions, starts a new session and sets the session cookies.",
		Tags:        []string{authenticationTag},
	}, routes.consumeLink)
}

func (routes *managementRoutes) listMembers(ctx context.Context, input *listMembersInput) (*httpapi.Page[MemberResponse], error) {
	environment, err := httpapi.EnvironmentFromContext(ctx)
	if err != nil {
		return nil, err
	}
	limit, err := httpapi.ParseLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	listed, nextCursor, err := routes.service.List(ctx, environment, input.Cursor, limit)
	if err != nil {
		return nil, err
	}
	responses := make([]MemberResponse, 0, len(listed))
	for _, member := range listed {
		responses = append(responses, NewMemberResponse(member))
	}
	return httpapi.NewPage(responses, nextCursor), nil
}

func (routes *managementRoutes) addMember(ctx context.Context, input *addMemberInput) (*addMemberOutput, error) {
	principal, err := httpapi.MemberPrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	member, linkURL, err := routes.service.Add(ctx, input.Body.Email, input.Body.DisplayName, principal.MemberID)
	if err != nil {
		return nil, err
	}
	return &addMemberOutput{Body: addedMemberResponse{Member: NewMemberResponse(member), LinkURL: linkURL}}, nil
}

func (routes *managementRoutes) removeMember(ctx context.Context, input *memberPathInput) (*struct{}, error) {
	principal, err := httpapi.MemberPrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	memberID, err := identifiers.Decode(identifiers.PrefixMember, input.MemberID)
	if err != nil {
		return nil, httpapi.ErrNotFound
	}
	if err := routes.service.Remove(ctx, principal.MemberID, memberID); err != nil {
		return nil, err
	}
	return &struct{}{}, nil
}

func (routes *managementRoutes) createResetLink(ctx context.Context, input *memberPathInput) (*resetLinkOutput, error) {
	principal, err := httpapi.MemberPrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	memberID, err := identifiers.Decode(identifiers.PrefixMember, input.MemberID)
	if err != nil {
		return nil, httpapi.ErrNotFound
	}
	linkURL, err := routes.service.CreateResetLink(ctx, memberID, &principal.MemberID)
	if err != nil {
		return nil, err
	}
	return &resetLinkOutput{Body: resetLinkResponse{LinkURL: linkURL}}, nil
}

func (routes *managementRoutes) inspectLink(ctx context.Context, input *inspectLinkInput) (*linkDetailsOutput, error) {
	details, err := routes.service.InspectLink(ctx, input.Body.Token)
	if err != nil {
		return nil, err
	}
	return &linkDetailsOutput{Body: linkDetailsResponse(details)}, nil
}

func (routes *managementRoutes) consumeLink(ctx context.Context, input *consumeLinkInput) (*sessionOutput, error) {
	member, err := routes.service.ConsumeLink(ctx, input.Body.Token, input.Body.Password)
	if err != nil {
		return nil, err
	}
	clientIP := input.ClientIP(routes.service.configuration.TrustedProxies)
	token, err := routes.service.sessions.Start(ctx, member.ID, input.UserAgent(), clientIP)
	if err != nil {
		return nil, err
	}
	return &sessionOutput{
		SetCookie: SessionCookies(token, routes.service.configuration.SecureCookies()),
		Body:      NewMemberResponse(member),
	}, nil
}
