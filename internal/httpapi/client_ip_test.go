package httpapi_test

import (
	"context"
	"net/http"
	"net/netip"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/httpapi"
)

type clientDetailsInput struct {
	httpapi.ClientDetails
}

type clientDetailsOutput struct {
	Body clientDetailsBody
}

type clientDetailsBody struct {
	ClientIP  string `json:"client_ip"`
	UserAgent string `json:"user_agent"`
}

func TestClientIP(t *testing.T) {
	trustedProxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")}
	tests := []struct {
		name           string
		remoteAddress  string
		forwardedFor   []string
		trustedProxies []netip.Prefix
		want           string
	}{
		{
			name:          "no trusted proxies uses the peer",
			remoteAddress: "203.0.113.7:51000",
			want:          "203.0.113.7",
		},
		{
			name:          "no trusted proxies ignores the header",
			remoteAddress: "203.0.113.7:51000",
			forwardedFor:  []string{"198.51.100.9"},
			want:          "203.0.113.7",
		},
		{
			name:           "untrusted peer with a spoofed header uses the peer",
			remoteAddress:  "203.0.113.7:51000",
			forwardedFor:   []string{"10.0.0.5, 198.51.100.9"},
			trustedProxies: trustedProxies,
			want:           "203.0.113.7",
		},
		{
			name:           "trusted peer uses the forwarded client",
			remoteAddress:  "10.0.0.2:51000",
			forwardedFor:   []string{"198.51.100.9"},
			trustedProxies: trustedProxies,
			want:           "198.51.100.9",
		},
		{
			name:           "trusted peer skips trusted hops",
			remoteAddress:  "10.0.0.2:51000",
			forwardedFor:   []string{"198.51.100.9, 10.0.0.5"},
			trustedProxies: trustedProxies,
			want:           "198.51.100.9",
		},
		{
			name:           "client spoofed entries left of the rightmost untrusted entry",
			remoteAddress:  "10.0.0.2:51000",
			forwardedFor:   []string{"192.0.2.44, 198.51.100.9"},
			trustedProxies: trustedProxies,
			want:           "198.51.100.9",
		},
		{
			name:           "entries across repeated headers",
			remoteAddress:  "10.0.0.2:51000",
			forwardedFor:   []string{"192.0.2.44", "198.51.100.9, 10.0.0.5"},
			trustedProxies: trustedProxies,
			want:           "198.51.100.9",
		},
		{
			name:           "trusted peer without the header uses the peer",
			remoteAddress:  "10.0.0.2:51000",
			trustedProxies: trustedProxies,
			want:           "10.0.0.2",
		},
		{
			name:           "every entry trusted uses the leftmost entry",
			remoteAddress:  "10.0.0.2:51000",
			forwardedFor:   []string{"10.0.0.9, 10.0.0.5"},
			trustedProxies: trustedProxies,
			want:           "10.0.0.9",
		},
		{
			name:           "unparsable entry stops at the last trusted address",
			remoteAddress:  "10.0.0.2:51000",
			forwardedFor:   []string{"198.51.100.9, not-an-address, 10.0.0.5"},
			trustedProxies: trustedProxies,
			want:           "10.0.0.5",
		},
		{
			name:           "IPv4-mapped peer matches an IPv4 proxy range",
			remoteAddress:  "[::ffff:10.0.0.2]:51000",
			forwardedFor:   []string{"198.51.100.9"},
			trustedProxies: trustedProxies,
			want:           "198.51.100.9",
		},
		{
			name:           "IPv6 proxy and client",
			remoteAddress:  "[fd00::2]:51000",
			forwardedFor:   []string{"2001:db8::7"},
			trustedProxies: trustedProxies,
			want:           "2001:db8::7",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := newRequest(t, http.MethodGet, "/api/v1/things", "")
			request.RemoteAddr = test.remoteAddress
			for _, value := range test.forwardedFor {
				request.Header.Add("X-Forwarded-For", value)
			}

			got := httpapi.ClientIP(request, test.trustedProxies)

			if got != netip.MustParseAddr(test.want) {
				t.Errorf("ClientIP = %s, want %s", got, test.want)
			}
		})
	}
}

func TestClientDetailsReachHandler(t *testing.T) {
	trustedProxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	server := newTestServer(t, httpapi.RejectingAuthenticator{})
	httpapi.Register(server.api, httpapi.RouteGroupPublic, huma.Operation{
		OperationID: "get-client",
		Method:      http.MethodGet,
		Path:        "/api/v1/client",
	}, func(_ context.Context, input *clientDetailsInput) (*clientDetailsOutput, error) {
		return &clientDetailsOutput{Body: clientDetailsBody{
			ClientIP:  input.ClientIP(trustedProxies).String(),
			UserAgent: input.UserAgent(),
		}}, nil
	})
	request := newRequest(t, http.MethodGet, "/api/v1/client", "")
	request.RemoteAddr = "10.0.0.2:51000"
	request.Header.Set("X-Forwarded-For", "198.51.100.9")
	request.Header.Set("User-Agent", "preburn-test/1.0")

	recorder := server.serve(request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	want := map[string]any{"client_ip": "198.51.100.9", "user_agent": "preburn-test/1.0"}
	if diff := cmp.Diff(want, decodeBody(t, recorder)); diff != "" {
		t.Errorf("body mismatch (-want +got):\n%s", diff)
	}
}
