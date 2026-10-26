// Package auth implements the Microsoft -> Xbox Live -> Minecraft login chain,
// using the device code flow (no embedded browser, no password ever touches
// mcli).
//
//	device code  -> login.microsoftonline.com (consumers)
//	MSA token    -> user.auth.xboxlive.com/user/authenticate
//	XSTS token   -> xsts.auth.xboxlive.com/xsts/authorize
//	MC token     -> api.minecraftservices.com/authentication/login_with_xbox
//	profile      -> api.minecraftservices.com/minecraft/profile
//
// Reference for the exact scopes/headers: MrShieh-X/console-minecraft-launcher.
package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
)

// ClientID is the public client id used by default. It is the one the reference
// launcher (console-minecraft-launcher) ships, read out of its release jar's
// manifest; Microsoft accepts it for the authorization-code + loopback flow but
// rejects it for device codes, which is why the browser flow is the default.
const ClientID = "288ec5dd-6736-4d4b-9b96-30e083a8cad2"

// Scope requests an Xbox Live ticket plus a refresh token.
const Scope = "service::user.auth.xboxlive.com::MBI_SSL offline_access"

// Endpoints.
const (
	deviceCodeURL = "https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode"
	tokenURL      = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	xblURL        = "https://user.auth.xboxlive.com/user/authenticate"
	xstsURL       = "https://xsts.auth.xboxlive.com/xsts/authorize"
	mcLoginURL    = "https://api.minecraftservices.com/authentication/login_with_xbox"
	mcProfileURL  = "https://api.minecraftservices.com/minecraft/profile"
)

// DeviceCode is what the user has to type into the browser.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Message         string `json:"message"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// MSTokens are the Microsoft OAuth tokens.
type MSTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

// MCTokens are the Minecraft session tokens.
type MCTokens struct {
	AccessToken string
	ExpiresAt   time.Time
}

// Profile is the Minecraft profile.
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Session is the full result of a login: everything a launch needs.
type Session struct {
	MS      MSTokens
	MC      MCTokens
	Profile Profile
	XUID    string
}

// Client performs the login chain.
type Client struct {
	HTTP    *http.Client
	Log     *logx.Logger
	Timeout time.Duration
	// ClientID overrides the built-in public client id. Different client ids
	// support different flows: the browser flow works with the id the reference
	// launcher uses, while a device-code capable id is needed for `--flow device`.
	ClientID string
	// TokenURL is where Refresh talks to. The browser flow uses
	// LiveTokenURL; the device flow uses the consumers v2 endpoint.
	TokenURL string
	// RefreshScope is sent with refresh requests. login.live.com wants no scope
	// on refresh, login.microsoftonline.com does.
	RefreshScope string
}

// UseBrowserFlow points the client at the login.live.com endpoints, which are
// the ones the default client id supports.
func (c *Client) UseBrowserFlow() {
	c.TokenURL = LiveTokenURL
	c.RefreshScope = ""
}

// clientID returns the effective OAuth client id.
func (c *Client) clientID() string {
	if c.ClientID != "" {
		return c.ClientID
	}
	return ClientID
}

// NewClient builds a client with sane defaults.
func NewClient(timeout time.Duration, log *logx.Logger) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		HTTP:    &http.Client{Timeout: timeout},
		Log:     log,
		Timeout: timeout,
	}
}

// ---------------------------------------------------------------------------
// HTTP plumbing

func (c *Client) post(url string, headers map[string]string, body interface{}, out interface{}) error {
	var reader io.Reader
	switch b := body.(type) {
	case nil:
		reader = nil
	case string:
		reader = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(http.MethodPost, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if _, ok := headers["Content-Type"]; !ok {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return &HTTPError{Status: resp.StatusCode, Body: string(data), URL: url}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s: %v", url, err)
		}
	}
	return nil
}

func (c *Client) get(url, bearer string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return &HTTPError{Status: resp.StatusCode, Body: string(data), URL: url}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s: %v", url, err)
		}
	}
	return nil
}

// HTTPError carries a failing status plus the provider's message.
type HTTPError struct {
	Status int
	Body   string
	URL    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s: HTTP %d %s", e.URL, e.Status, firstLine(e.Body))
}

func firstLine(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

func (c *Client) log() *logx.Logger {
	if c.Log != nil {
		return c.Log
	}
	return logx.Discard()
}

// ---------------------------------------------------------------------------
// device code flow

// RequestDeviceCode starts the flow.
func (c *Client) RequestDeviceCode() (*DeviceCode, error) {
	form := "client_id=" + c.clientID() + "&scope=" + strings.ReplaceAll(Scope, " ", "+")
	dev := &DeviceCode{}
	err := c.post(deviceCodeURL, map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}, form, dev)
	if err != nil {
		// Retry once through the JSON form, some tenants prefer it.
		if err2 := c.post(deviceCodeURL, nil, map[string]string{"client_id": c.clientID(), "scope": Scope}, dev); err2 != nil {
			return nil, fmt.Errorf("%s", i18n.T("auth.device_failed", err))
		}
	}
	if dev.UserCode == "" {
		return nil, fmt.Errorf("%s", i18n.T("auth.device_failed", "empty device code response"))
	}
	if dev.Interval <= 0 {
		dev.Interval = 5
	}
	c.log().Infof("%s", i18n.T("auth.device_code", dev.VerificationURI, dev.UserCode))
	c.log().Infof("%s", i18n.T("auth.waiting"))
	return dev, nil
}

// WaitForToken polls until the user authorizes, the code expires, or timeout
// elapses (timeout 0 = until the code expires).
func (c *Client) WaitForToken(dev *DeviceCode, timeout time.Duration) (*MSTokens, error) {
	deadline := time.Now().Add(time.Duration(dev.ExpiresIn) * time.Second)
	if timeout > 0 {
		if t := time.Now().Add(timeout); t.Before(deadline) {
			deadline = t
		}
	}
	interval := time.Duration(dev.Interval) * time.Second
	if interval < 2*time.Second {
		interval = 2 * time.Second
	}
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%s", i18n.T("auth.device_expired"))
		}
		form := "grant_type=urn:ietf:params:oauth:grant-type:device_code" +
			"&client_id=" + c.clientID() +
			"&device_code=" + dev.DeviceCode
		var out struct {
			AccessToken      string `json:"access_token"`
			RefreshToken     string `json:"refresh_token"`
			ExpiresIn        int    `json:"expires_in"`
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		err := c.post(tokenURL, map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		}, form, &out)
		if err != nil {
			var he *HTTPError
			if ok := asHTTPError(err, &he); ok {
				switch he.ErrorCode() {
				case "authorization_pending":
					time.Sleep(interval)
					continue
				case "slow_down":
					interval += 2 * time.Second
					time.Sleep(interval)
					continue
				case "authorization_declined":
					return nil, fmt.Errorf("%s", i18n.T("core.cancelled"))
				case "expired_token":
					return nil, fmt.Errorf("%s", i18n.T("auth.device_expired"))
				}
			}
			return nil, fmt.Errorf("%s", i18n.T("auth.poll_failed", err))
		}
		if out.AccessToken == "" {
			time.Sleep(interval)
			continue
		}
		return &MSTokens{AccessToken: out.AccessToken, RefreshToken: out.RefreshToken, ExpiresIn: out.ExpiresIn}, nil
	}
}

// ErrorCode extracts the OAuth error code from an HTTP error body.
func (e *HTTPError) ErrorCode() string {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(e.Body), &body); err == nil {
		return body.Error
	}
	return ""
}

// XSTS error codes, named because a bare seven-digit number in a switch says
// nothing about what Microsoft means by it.
const (
	xstsNoAccount     int64 = 2148916233
	xstsRegionBlocked int64 = 2148916235
	xstsChildAccount  int64 = 2148916238
)

func asHTTPError(err error, out **HTTPError) bool {
	if he, ok := err.(*HTTPError); ok {
		*out = he
		return true
	}
	return false
}

// Refresh exchanges a refresh token for fresh MSA tokens.
func (c *Client) Refresh(refreshToken string) (*MSTokens, error) {
	endpoint := c.TokenURL
	if endpoint == "" {
		endpoint = tokenURL
	}
	scope := c.RefreshScope
	if scope == "" && c.TokenURL == "" {
		scope = Scope // the device-flow endpoint requires the scope on refresh
	}
	form := "grant_type=refresh_token&client_id=" + url.QueryEscape(c.clientID()) +
		"&refresh_token=" + url.QueryEscape(refreshToken)
	if scope != "" {
		form += "&scope=" + strings.ReplaceAll(scope, " ", "+")
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := c.post(endpoint, map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}, form, &out); err != nil {
		return nil, fmt.Errorf("%s", i18n.T("auth.refresh_failed", err))
	}
	tokens := &MSTokens{AccessToken: out.AccessToken, RefreshToken: out.RefreshToken, ExpiresIn: out.ExpiresIn}
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = refreshToken
	}
	return tokens, nil
}

// ---------------------------------------------------------------------------
// xbox live chain

type xblResponse struct {
	Token         string `json:"Token"`
	DisplayClaims struct {
		XUI []struct {
			UHS string `json:"uhs"`
		} `json:"xui"`
	} `json:"DisplayClaims"`
	XErr  int    `json:"XErr"`
	Error string `json:"error"`
	Msg   string `json:"Message"`
}

// XboxLive authenticates the MSA token against Xbox Live.
func (c *Client) XboxLive(msaToken string) (token, uhs string, err error) {
	body := map[string]interface{}{
		"Properties": map[string]interface{}{
			"AuthMethod": "RPS",
			"SiteName":   "user.auth.xboxlive.com",
			"RpsTicket":  "d=" + msaToken,
		},
		"RelyingParty": "http://auth.xboxlive.com",
		"TokenType":    "JWT",
	}
	var out xblResponse
	if err := c.post(xblURL, nil, body, &out); err != nil {
		return "", "", fmt.Errorf("%s", i18n.T("auth.xbl_failed", err))
	}
	if out.Token == "" || len(out.DisplayClaims.XUI) == 0 {
		return "", "", fmt.Errorf("%s", i18n.T("auth.xbl_failed", "empty response"))
	}
	return out.Token, out.DisplayClaims.XUI[0].UHS, nil
}

// XSTS exchanges the Xbox Live token for an XSTS token scoped to Minecraft.
func (c *Client) XSTS(xblToken string) (token, uhs string, err error) {
	body := map[string]interface{}{
		"Properties": map[string]interface{}{
			"SandboxId":  "RETAIL",
			"UserTokens": []string{xblToken},
		},
		"RelyingParty": "rp://api.minecraftservices.com/",
		"TokenType":    "JWT",
	}
	var out xblResponse
	if err := c.post(xstsURL, nil, body, &out); err != nil {
		var he *HTTPError
		if ok := asHTTPError(err, &he); ok {
			var payload struct {
				XErr     int64  `json:"XErr"`
				Message  string `json:"Message"`
				Redirect string `json:"Redirect"`
			}
			_ = json.Unmarshal([]byte(he.Body), &payload)
			// These codes are larger than a 32-bit int, so they are compared as
			// int64: that is what made every 32-bit build fail to compile.
			switch payload.XErr {
			case xstsNoAccount:
				return "", "", fmt.Errorf("%s", i18n.T("auth.xsts_no_account"))
			case xstsRegionBlocked:
				return "", "", fmt.Errorf("%s", i18n.T("auth.xsts_region"))
			case xstsChildAccount:
				return "", "", fmt.Errorf("%s", i18n.T("auth.xsts_child"))
			}
			return "", "", fmt.Errorf("%s", i18n.T("auth.xsts_failed", err, payload.XErr))
		}
		return "", "", fmt.Errorf("%s", i18n.T("auth.xsts_failed", err, 0))
	}
	if out.Token == "" || len(out.DisplayClaims.XUI) == 0 {
		return "", "", fmt.Errorf("%s", i18n.T("auth.xsts_failed", "empty response", out.XErr))
	}
	return out.Token, out.DisplayClaims.XUI[0].UHS, nil
}

// MineLogin exchanges the XSTS token for a Minecraft access token.
func (c *Client) MineLogin(xstsToken, uhs string) (*MCTokens, error) {
	body := map[string]string{"identityToken": "XBL3.0 x=" + uhs + ";" + xstsToken}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := c.post(mcLoginURL, nil, body, &out); err != nil {
		return nil, fmt.Errorf("%s", i18n.T("auth.mc_login_failed", err))
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("%s", i18n.T("auth.mc_login_failed", "empty access token"))
	}
	if out.ExpiresIn == 0 {
		out.ExpiresIn = 86400
	}
	return &MCTokens{
		AccessToken: out.AccessToken,
		ExpiresAt:   time.Now().Add(time.Duration(out.ExpiresIn) * time.Second),
	}, nil
}

// MineProfile reads the account's Minecraft profile (also proves ownership).
func (c *Client) MineProfile(mcToken string) (*Profile, error) {
	prof := &Profile{}
	err := c.get(mcProfileURL, mcToken, prof)
	if err != nil {
		var he *HTTPError
		if ok := asHTTPError(err, &he); ok && (he.Status == 404 || he.Status == 401) {
			return nil, fmt.Errorf("%s", i18n.T("auth.no_profile"))
		}
		return nil, fmt.Errorf("%s", i18n.T("auth.profile_failed", err))
	}
	return prof, nil
}

// ---------------------------------------------------------------------------
// high level

// Login runs the whole chain starting from MSA tokens.
func (c *Client) Login(ms *MSTokens) (*Session, error) {
	if ms == nil || ms.AccessToken == "" {
		return nil, fmt.Errorf("%s", i18n.T("auth.poll_failed", "no microsoft access token"))
	}
	xblToken, uhs, err := c.XboxLive(ms.AccessToken)
	if err != nil {
		return nil, err
	}
	xstsToken, xuhs, err := c.XSTS(xblToken)
	if err != nil {
		return nil, err
	}
	if xuhs != "" {
		uhs = xuhs
	}
	mc, err := c.MineLogin(xstsToken, uhs)
	if err != nil {
		return nil, err
	}
	prof, err := c.MineProfile(mc.AccessToken)
	if err != nil {
		return nil, err
	}
	return &Session{MS: *ms, MC: *mc, Profile: *prof, XUID: uhs}, nil
}

// Refresh re-runs the chain from a stored refresh token.
func (c *Client) RefreshSession(refreshToken string) (*Session, error) {
	ms, err := c.Refresh(refreshToken)
	if err != nil {
		return nil, err
	}
	return c.Login(ms)
}
