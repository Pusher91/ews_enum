package ews

import (
	"crypto/tls"
	"net/http"
	"net/http/cookiejar"
	"time"

	"github.com/Azure/go-ntlmssp"
)

// ClientOpts configures the HTTP client behavior.
type ClientOpts struct {
	NTLM             bool
	TimeoutSec       int
	MaxConns         int
	UseCookies       bool
	DisableKeepAlive bool // Force new TCP connection per request (needed for NTLM spray)
}

// NewClient creates an HTTP client configured for EWS access.
func NewClient(opts ClientOpts) *http.Client {
	tlsConfig := &tls.Config{InsecureSkipVerify: true}

	transport := &http.Transport{
		TLSClientConfig:     tlsConfig,
		MaxIdleConns:        opts.MaxConns,
		MaxIdleConnsPerHost: opts.MaxConns,
		MaxConnsPerHost:     opts.MaxConns,
		DisableKeepAlives:   opts.DisableKeepAlive,
	}

	var rt http.RoundTripper = transport
	if opts.NTLM {
		rt = ntlmssp.Negotiator{RoundTripper: transport}
	}

	client := &http.Client{
		Transport: rt,
		Timeout:   time.Duration(opts.TimeoutSec) * time.Second,
	}

	if opts.UseCookies {
		jar, _ := cookiejar.New(nil)
		client.Jar = jar
	}

	return client
}
