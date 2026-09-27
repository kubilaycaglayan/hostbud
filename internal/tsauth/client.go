// Package tsauth verifies a client's Tailscale identity through tailscaled's LocalAPI.
package tsauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client queries the local tailscaled socket. Requests are bounded to two seconds.
type Client struct{ http *http.Client }

var ErrUnknown = errors.New("tailscale whois has no matching node")

func New(socket string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socket)
	}}
	return &Client{http: &http.Client{Transport: tr, Timeout: 2 * time.Second}}
}

// Login queries LocalAPI whois for an address and returns its login or tag.
func (c *Client) Login(ctx context.Context, addr string) (string, error) {
	u := "http://local-tailscaled.sock/localapi/v0/whois?addr=" + url.QueryEscape(addr)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Host = "local-tailscaled.sock"
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrUnknown
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tailscale whois status %d", resp.StatusCode)
	}
	var body struct {
		UserProfile struct {
			LoginName string `json:"loginName"`
		} `json:"UserProfile"`
		Node struct {
			Tags []string `json:"Tags"`
		} `json:"Node"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.UserProfile.LoginName != "" {
		return strings.ToLower(body.UserProfile.LoginName), nil
	}
	for _, tag := range body.Node.Tags {
		if strings.HasPrefix(tag, "tag:") {
			return strings.ToLower(tag), nil
		}
	}
	return "", nil
}

// Allowed compares a normalized login against the comma-separated allowlist.
func Allowed(list, login string) bool {
	login = strings.ToLower(strings.TrimSpace(login))
	if login == "" {
		return false
	}
	for _, item := range strings.Split(list, ",") {
		if strings.EqualFold(strings.TrimSpace(item), login) {
			return true
		}
	}
	return false
}
