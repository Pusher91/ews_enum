package ews

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/Azure/go-ntlmssp"
)

// NewClient creates an HTTP client configured for EWS access.
// If ntlm is true, wraps the transport with NTLM negotiation.
func NewClient(ntlm bool, timeoutSec int) *http.Client {
	tlsConfig := &tls.Config{InsecureSkipVerify: true}

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	var rt http.RoundTripper = transport
	if ntlm {
		rt = ntlmssp.Negotiator{RoundTripper: transport}
	}

	return &http.Client{
		Transport: rt,
		Timeout:   time.Duration(timeoutSec) * time.Second,
	}
}
