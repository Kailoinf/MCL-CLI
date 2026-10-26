package auth

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
)

// Browser (authorization code + loopback) flow.
//
// Facts verified against the reference launcher
// (MrShieh-X/console-minecraft-launcher v2.2.2, modules/account/authentication/
// microsoft/MicrosoftAuthentication.java): it does NOT use the device code flow.
// It opens login.live.com with response_type=code, listens on localhost ports
// 29116-29126, and exchanges the code at login.live.com/oauth20_token.srf — no
// client secret, no PKCE. The client id it uses (read out of the release jar's
// manifest) is registered at Microsoft but is *not* enabled for device codes,
// which is why mcli offers both flows.
const (
	// LiveAuthorizeURL is the consumer authorize endpoint.
	LiveAuthorizeURL = "https://login.live.com/oauth20_authorize.srf"
	// LiveTokenURL exchanges and refreshes tokens for the browser flow.
	LiveTokenURL = "https://login.live.com/oauth20_token.srf"
	// BrowserScope is the scope set the reference implementation requests.
	BrowserScope = "XboxLive.signin offline_access"
	// LoopbackPath is the redirect path the local server answers on.
	LoopbackPath = "/authentication-response"
	// loopbackPortMin/Max bound the port search, matching the reference.
	loopbackPortMin = 29116
	loopbackPortMax = 29126
)

// BrowserLogin runs the authorization-code flow: it starts a loopback server,
// hands the authorize URL to onURL, waits for the browser to come back with a
// code and exchanges it for Microsoft tokens.
func (c *Client) BrowserLogin(timeout time.Duration, onURL func(string)) (*MSTokens, error) {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	port, listener, err := listenLoopback()
	if err != nil {
		return nil, err
	}
	redirect := fmt.Sprintf("http://localhost:%d%s", port, LoopbackPath)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(LoopbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			desc := strings.TrimSpace(e + " " + q.Get("error_description"))
			c.log().Warnf("%s", i18n.T("auth.browser_failed", desc))
			fmt.Fprintf(w, "%s", i18n.T("auth.page_fail", desc))
			select {
			case errCh <- fmt.Errorf("%s", i18n.T("auth.browser_failed", desc)):
			default:
			}
			return
		}
		code := q.Get("code")
		if code == "" {
			fmt.Fprintf(w, "%s", i18n.T("auth.page_fail", i18n.T("auth.no_code")))
			select {
			case errCh <- fmt.Errorf("%s", i18n.T("auth.browser_failed", i18n.T("auth.no_code"))):
			default:
			}
			return
		}
		fmt.Fprintf(w, "%s", i18n.T("auth.page_ok"))
		select {
		case codeCh <- code:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(listener) }()
	defer func() { _ = srv.Close() }()

	authURL := LiveAuthorizeURL +
		"?client_id=" + url.QueryEscape(c.clientID()) +
		"&response_type=code" +
		"&scope=" + strings.ReplaceAll(BrowserScope, " ", "+") +
		"&prompt=select_account" +
		"&redirect_uri=" + url.QueryEscape(redirect)
	if onURL != nil {
		onURL(authURL)
	}

	c.log().Infof("%s", i18n.T("auth.browser_waiting", redirect))
	c.log().Infof("%s", i18n.T("auth.port_hint", port))
	select {
	case code := <-codeCh:
		return c.exchangeCode(code, redirect)
	case err := <-errCh:
		return nil, err
	case <-time.After(timeout):
		return nil, fmt.Errorf("%s", i18n.T("auth.browser_timeout", timeout.Round(time.Second)))
	}
}

// exchangeCode trades the authorization code for tokens on the live endpoint.
func (c *Client) exchangeCode(code, redirect string) (*MSTokens, error) {
	form := "client_id=" + url.QueryEscape(c.clientID()) +
		"&grant_type=authorization_code" +
		"&scope=" + strings.ReplaceAll(BrowserScope, " ", "+") +
		"&redirect_uri=" + url.QueryEscape(redirect) +
		"&code=" + url.QueryEscape(code)
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		Desc         string `json:"error_description"`
	}
	if err := c.post(LiveTokenURL, map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}, form, &out); err != nil {
		return nil, fmt.Errorf("%s", i18n.T("auth.exchange_failed", err))
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("%s", i18n.T("auth.exchange_failed", out.Error+" "+out.Desc))
	}
	return &MSTokens{AccessToken: out.AccessToken, RefreshToken: out.RefreshToken, ExpiresIn: out.ExpiresIn}, nil
}

// listenLoopback binds the first free port in the reference range, falling back
// to an ephemeral port when the whole range is taken.
func listenLoopback() (int, net.Listener, error) {
	for port := loopbackPortMin; port <= loopbackPortMax; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return port, ln, nil
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, fmt.Errorf("cannot listen for the login redirect: %w", err)
	}
	return ln.Addr().(*net.TCPAddr).Port, ln, nil
}

// OpenBrowser makes a best-effort attempt to open a URL. It is deliberately
// quiet: on a headless machine (or a remote shell) the URL is simply printed.
func OpenBrowser(rawURL string, log *logx.Logger) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			if log != nil {
				log.Debugf("%s", i18n.T("auth.browser_headless"))
			}
			return
		}
		cmd = exec.Command("xdg-open", rawURL)
	}
	if err := cmd.Start(); err != nil && log != nil {
		log.Debugf("%s", i18n.T("auth.browser_open_failed", err))
	}
}
