// Package githubauth implements the GitHub OAuth Device Flow for desktop login.
// Reference: https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#device-flow
package githubauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ClientID is the registered GitHub OAuth App client_id.
//
// Override at startup via env var PRISMCONDUCTOR_GH_CLIENT_ID or by editing this constant.
// Register your OAuth App at https://github.com/settings/developers
// (any HTTPS callback URL works — device flow doesn't use it; "https://example.com" is fine).
const DefaultClientID = "REPLACE_ME"

// Scopes needed for issue read/write across the user's repos.
const Scopes = "repo,read:org"

type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type Token struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	Scope       string    `json:"scope"`
	ObtainedAt  time.Time `json:"obtained_at"`
}

type Client struct {
	ClientID string
	HTTP     *http.Client
}

func New(clientID string) *Client {
	if clientID == "" {
		clientID = os.Getenv("PRISMCONDUCTOR_GH_CLIENT_ID")
	}
	if clientID == "" {
		clientID = DefaultClientID
	}
	return &Client{ClientID: clientID, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// RequestDeviceCode kicks off the device flow. Returns the code the user must enter
// at VerificationURI in their browser.
func (c *Client) RequestDeviceCode(ctx context.Context) (*DeviceCode, error) {
	if c.ClientID == "" || c.ClientID == "REPLACE_ME" {
		return nil, errors.New("github OAuth App client_id not configured (set PRISMCONDUCTOR_GH_CLIENT_ID or edit DefaultClientID)")
	}
	form := url.Values{}
	form.Set("client_id", c.ClientID)
	form.Set("scope", Scopes)
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/device/code", strings.NewReader(form.Encode()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var dc DeviceCode
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, err
	}
	if dc.DeviceCode == "" {
		return nil, fmt.Errorf("github device code request failed (status %d)", resp.StatusCode)
	}
	return &dc, nil
}

// PollForToken polls until the user authorizes (or the code expires).
// Honors the device flow's "interval" + "slow_down" backoff per the spec.
func (c *Client) PollForToken(ctx context.Context, dc *DeviceCode) (*Token, error) {
	interval := time.Duration(dc.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		form := url.Values{}
		form.Set("client_id", c.ClientID)
		form.Set("device_code", dc.DeviceCode)
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		req, _ := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		var body struct {
			AccessToken      string `json:"access_token"`
			TokenType        string `json:"token_type"`
			Scope            string `json:"scope"`
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
			Interval         int    `json:"interval"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if body.AccessToken != "" {
			return &Token{
				AccessToken: body.AccessToken,
				TokenType:   body.TokenType,
				Scope:       body.Scope,
				ObtainedAt:  time.Now(),
			}, nil
		}
		switch body.Error {
		case "authorization_pending":
			// keep polling
		case "slow_down":
			interval += 5 * time.Second
		case "expired_token":
			return nil, errors.New("device code expired; please retry login")
		case "access_denied":
			return nil, errors.New("authorization denied by user")
		case "":
			// no error and no token — keep going
		default:
			return nil, fmt.Errorf("device flow error: %s — %s", body.Error, body.ErrorDescription)
		}
	}
	return nil, errors.New("device code expired")
}

// --- Token persistence ---

// TokenPath returns the on-disk path for the cached token.
//
// SECURITY: Phase 1.5 should move this to OS keychain (macOS Keychain via
// github.com/zalando/go-keyring). For now plaintext under the app's config dir,
// 0600 perms. Same pattern `gh` CLI used before keychain support landed.
func TokenPath(configDir string) string {
	return filepath.Join(configDir, "github_token.json")
}

func SaveToken(configDir string, t *Token) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(TokenPath(configDir), b, 0o600)
}

func LoadToken(configDir string) (*Token, error) {
	b, err := os.ReadFile(TokenPath(configDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var t Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func ClearToken(configDir string) error {
	err := os.Remove(TokenPath(configDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
