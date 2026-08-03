package egressproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"time"
)

const (
	connectPort    = "443"
	resolveTimeout = 5 * time.Second
	dialTimeout    = 5 * time.Second
)

// Resolver is the bounded DNS dependency used before every tunnel.
type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

// Dialer opens only the already-resolved validated IP selected by the proxy.
type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

// Server is one exact-policy CONNECT-only HTTP handler.
type Server struct {
	policy   Policy
	resolver Resolver
	dialer   Dialer
}

// New constructs one fail-closed proxy with no configurable destination.
func New(policy Policy) (*Server, error) {
	if _, err := Hosts(policy); err != nil {
		return nil, err
	}
	return &Server{
		policy:   policy,
		resolver: net.DefaultResolver,
		dialer:   &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second},
	}, nil
}

func newWithNetwork(policy Policy, resolver Resolver, dialer Dialer) (*Server, error) {
	if _, err := Hosts(policy); err != nil || resolver == nil || dialer == nil {
		return nil, proxyError("configuration_invalid")
	}
	return &Server{policy: policy, resolver: resolver, dialer: dialer}, nil
}

// ServeHTTP accepts only an exact allowlisted host on port 443.
func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodConnect {
		http.Error(writer, "method_rejected", http.StatusMethodNotAllowed)
		return
	}
	host, err := validatedTarget(server.policy, request)
	if err != nil {
		http.Error(writer, "destination_rejected", http.StatusForbidden)
		return
	}
	address, err := server.resolve(request.Context(), host)
	if err != nil {
		http.Error(writer, "resolution_rejected", http.StatusBadGateway)
		return
	}
	upstream, err := server.dialer.DialContext(request.Context(), "tcp", address)
	if err != nil {
		http.Error(writer, "connect_failed", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		http.Error(writer, "tunnel_unavailable", http.StatusInternalServerError)
		return
	}
	downstream, buffered, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer downstream.Close()
	if _, err = io.WriteString(downstream, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	proxyTunnel(request.Context(), downstream, buffered, upstream)
}

func validatedTarget(policy Policy, request *http.Request) (string, error) {
	if request == nil || request.URL == nil || request.URL.User != nil ||
		request.URL.RawQuery != "" || request.URL.Fragment != "" ||
		request.URL.Path != "" || request.RequestURI == "" ||
		strings.ContainsAny(request.RequestURI, "/?#@") {
		return "", proxyError("target_invalid")
	}
	host, port, err := net.SplitHostPort(request.Host)
	if err != nil || port != connectPort || host == "" || host != strings.ToLower(host) ||
		net.ParseIP(host) != nil || !hostAllowed(policy, host) ||
		request.RequestURI != net.JoinHostPort(host, port) {
		return "", proxyError("target_invalid")
	}
	return host, nil
}

func (server *Server) resolve(parent context.Context, host string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, resolveTimeout)
	defer cancel()
	addresses, err := server.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 || len(addresses) > 32 {
		return "", proxyError("dns_invalid")
	}
	unique := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !allowedAddress(address) {
			return "", proxyError("address_rejected")
		}
		unique[address] = struct{}{}
	}
	addresses = addresses[:0]
	for address := range unique {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(left, right int) bool {
		return addresses[left].Compare(addresses[right]) < 0
	})
	return net.JoinHostPort(addresses[0].String(), connectPort), nil
}

func allowedAddress(address netip.Addr) bool {
	return address.IsValid() && address.IsGlobalUnicast() && !address.IsPrivate() &&
		!address.IsLoopback() && !address.IsLinkLocalUnicast() &&
		!address.IsLinkLocalMulticast() && !address.IsMulticast() &&
		!address.IsUnspecified()
}

func proxyTunnel(ctx context.Context, downstream net.Conn, buffered *bufio.ReadWriter, upstream net.Conn) {
	done := make(chan struct{}, 2)
	copyStream := func(destination io.Writer, source io.Reader) {
		_, _ = io.Copy(destination, source)
		done <- struct{}{}
	}
	go copyStream(upstream, buffered)
	go copyStream(downstream, upstream)
	select {
	case <-ctx.Done():
	case <-done:
	}
}

// Error is one bounded proxy-policy failure without a destination payload.
type Error struct{ Code string }

// Error returns the bounded proxy failure code.
func (failure *Error) Error() string { return fmt.Sprintf("egress_proxy:%s", failure.Code) }

func proxyError(code string) error { return &Error{Code: code} }
