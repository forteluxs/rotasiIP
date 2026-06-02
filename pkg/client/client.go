// Package client provides a transparent HTTP client that routes all requests
// through a running mubeng proxy rotator.
package client

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"time"
)

// Client is a drop-in http.Client replacement that sends every request
// through the mubeng rotator at ProxyAddr.
type Client struct {
	*http.Client
	ProxyAddr string
}

// New creates a Client routed through proxyAddr (e.g. "http://127.0.0.1:8080").
func New(proxyAddr string, timeout time.Duration) (*Client, error) {
	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		return nil, err
	}

	return &Client{
		ProxyAddr: proxyAddr,
		Client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy:           http.ProxyURL(proxyURL),
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}, nil
}

// Get is a convenience wrapper for GET requests.
func (c *Client) Get(rawURL string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}
