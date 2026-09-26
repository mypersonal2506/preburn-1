package httpapi

import (
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

const forwardedForHeader = "X-Forwarded-For"

// ClientDetails gives a handler the client address and User-Agent of its
// request, such as login recording where a session started. Embed it in a
// Huma input struct, and Huma fills it before the handler runs.
type ClientDetails struct {
	request *http.Request
}

// ClientIP returns the address of the client that sent request. It is the
// peer address of the connection unless trustedProxies holds the peer. Then
// ClientIP walks the X-Forwarded-For entries of every such header from right
// to left and returns the first entry outside trustedProxies. When an entry
// is not a plain IP address, or every entry is trusted, it returns the last
// trusted address it walked. IPv4-mapped IPv6 addresses are returned as IPv4
// and zones are dropped. It returns the zero Addr only when RemoteAddr is not
// an address and port, which a TCP listener never produces.
func ClientIP(request *http.Request, trustedProxies []netip.Prefix) netip.Addr {
	peer, err := netip.ParseAddrPort(request.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	client := canonicalAddress(peer.Addr())
	if !isTrustedProxy(client, trustedProxies) {
		return client
	}
	var entries []string
	for _, value := range request.Header.Values(forwardedForHeader) {
		entries = append(entries, strings.Split(value, ",")...)
	}
	for index := len(entries) - 1; index >= 0; index-- {
		address, err := netip.ParseAddr(strings.TrimSpace(entries[index]))
		if err != nil {
			return client
		}
		address = canonicalAddress(address)
		if !isTrustedProxy(address, trustedProxies) {
			return address
		}
		client = address
	}
	return client
}

// Resolve keeps the request for ClientIP and UserAgent. Huma calls it before
// the handler runs.
func (details *ClientDetails) Resolve(ctx huma.Context) []error {
	details.request, _ = humago.Unwrap(ctx)
	return nil
}

// ClientIP returns the client address of the request, as the package
// function ClientIP determines it with trustedProxies.
func (details *ClientDetails) ClientIP(trustedProxies []netip.Prefix) netip.Addr {
	return ClientIP(details.request, trustedProxies)
}

// UserAgent returns the User-Agent header of the request, or an empty string
// when it has none.
func (details *ClientDetails) UserAgent() string {
	return details.request.UserAgent()
}

func canonicalAddress(address netip.Addr) netip.Addr {
	return address.Unmap().WithZone("")
}

func isTrustedProxy(address netip.Addr, trustedProxies []netip.Prefix) bool {
	return slices.ContainsFunc(trustedProxies, func(prefix netip.Prefix) bool {
		return prefix.Contains(address)
	})
}
