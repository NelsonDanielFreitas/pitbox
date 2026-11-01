package auth

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// IssuerDial returns a transport that sends the requests for exactly the issuer's host and
// port to a fixed loopback address over plain HTTP, keeping the original Host header. Every
// other destination goes through base untouched.
//
// Why: in the phone profile (ADR 0029) the issuer is https://<machine>.<tailnet>.ts.net:8443,
// served by "tailscale serve". The machine that runs tailscaled cannot reach its own tailnet
// address, so pitbox-api would time out on discovery. The identity provider picks its instance
// by Host, so talking to 127.0.0.1:<port> with the issuer's Host gives the same answers.
//
// It is local-only: the config refuses PITBOX_OIDC_LOCAL_DIAL outside PITBOX_ENV=local, and
// this function repeats the loopback and *.ts.net checks so it cannot be misused by a caller.
func IssuerDial(base http.RoundTripper, issuer, dial string, isTailnetHost func(string) bool) (http.RoundTripper, error) {
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return nil, errors.New("auth: issuer dial needs an https issuer")
	}
	if !isTailnetHost(u.Hostname()) {
		return nil, errors.New("auth: issuer dial is only for *.ts.net issuers")
	}
	host, _, err := net.SplitHostPort(dial)
	if err != nil || host != "127.0.0.1" {
		return nil, errors.New("auth: issuer dial must be 127.0.0.1:<port>")
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &issuerDial{base: base, host: strings.ToLower(u.Hostname()), port: port, dial: dial}, nil
}

type issuerDial struct {
	base       http.RoundTripper
	host, port string
	dial       string
}

func (t *issuerDial) RoundTrip(req *http.Request) (*http.Response, error) {
	port := req.URL.Port()
	if port == "" && req.URL.Scheme == "https" {
		port = "443"
	}
	if req.URL.Scheme != "https" || strings.ToLower(req.URL.Hostname()) != t.host || port != t.port {
		return t.base.RoundTrip(req)
	}
	out := req.Clone(req.Context())
	out.Host = req.URL.Host
	out.URL.Scheme = "http"
	out.URL.Host = t.dial
	return t.base.RoundTrip(out)
}
