package management

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestAPICallTransportDirectBypassesGlobalProxy(t *testing.T) {
	t.Parallel()

	h := &Handler{
		cfg: &config.Config{
			SDKConfig: sdkconfig.SDKConfig{ProxyURL: "http://global-proxy.example.com:8080"},
		},
	}

	transport := h.apiCallTransport(&coreauth.Auth{ProxyURL: "direct"})
	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", transport)
	}
	if httpTransport.Proxy != nil {
		t.Fatal("expected direct transport to disable proxy function")
	}
}

func TestAPICallTransportInvalidAuthFallsBackToGlobalProxy(t *testing.T) {
	t.Parallel()

	h := &Handler{
		cfg: &config.Config{
			SDKConfig: sdkconfig.SDKConfig{ProxyURL: "http://global-proxy.example.com:8080"},
		},
	}

	transport := h.apiCallTransport(&coreauth.Auth{ProxyURL: "bad-value"})
	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", transport)
	}

	req, errRequest := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if errRequest != nil {
		t.Fatalf("http.NewRequest returned error: %v", errRequest)
	}

	proxyURL, errProxy := httpTransport.Proxy(req)
	if errProxy != nil {
		t.Fatalf("httpTransport.Proxy returned error: %v", errProxy)
	}
	if proxyURL == nil || proxyURL.String() != "http://global-proxy.example.com:8080" {
		t.Fatalf("proxy URL = %v, want http://global-proxy.example.com:8080", proxyURL)
	}
}

func TestAPICallTransportAPIKeyAuthFallsBackToConfigProxyURL(t *testing.T) {
	t.Parallel()

	h := &Handler{
		cfg: &config.Config{
			SDKConfig: sdkconfig.SDKConfig{ProxyURL: "http://global-proxy.example.com:8080"},
			GeminiKey: []config.GeminiKey{{
				APIKey:   "gemini-key",
				ProxyURL: "http://gemini-proxy.example.com:8080",
			}},
			ClaudeKey: []config.ClaudeKey{{
				APIKey:   "claude-key",
				ProxyURL: "http://claude-proxy.example.com:8080",
			}},
			CodexKey: []config.CodexKey{{
				APIKey:   "codex-key",
				ProxyURL: "http://codex-proxy.example.com:8080",
			}},
			XAIKey: []config.XAIKey{{
				APIKey:   "xai-key",
				ProxyURL: "http://xai-proxy.example.com:8080",
			}},
			OpenAICompatibility: []config.OpenAICompatibility{{
				Name:    "bohe",
				BaseURL: "https://bohe.example.com",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{{
					APIKey:   "compat-key",
					ProxyURL: "http://compat-proxy.example.com:8080",
				}},
			}},
		},
	}

	cases := []struct {
		name      string
		auth      *coreauth.Auth
		wantProxy string
	}{
		{
			name: "gemini",
			auth: &coreauth.Auth{
				Provider:   "gemini",
				Attributes: map[string]string{"api_key": "gemini-key"},
			},
			wantProxy: "http://gemini-proxy.example.com:8080",
		},
		{
			name: "claude",
			auth: &coreauth.Auth{
				Provider:   "claude",
				Attributes: map[string]string{"api_key": "claude-key"},
			},
			wantProxy: "http://claude-proxy.example.com:8080",
		},
		{
			name: "codex",
			auth: &coreauth.Auth{
				Provider:   "codex",
				Attributes: map[string]string{"api_key": "codex-key"},
			},
			wantProxy: "http://codex-proxy.example.com:8080",
		},
		{
			name: "xai",
			auth: &coreauth.Auth{
				Provider:   "xai",
				Attributes: map[string]string{"api_key": "xai-key"},
			},
			wantProxy: "http://xai-proxy.example.com:8080",
		},
		{
			name: "openai-compatibility",
			auth: &coreauth.Auth{
				Provider: "bohe",
				Attributes: map[string]string{
					"api_key":      "compat-key",
					"compat_name":  "bohe",
					"provider_key": "bohe",
				},
			},
			wantProxy: "http://compat-proxy.example.com:8080",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			transport := h.apiCallTransport(tc.auth)
			httpTransport, ok := transport.(*http.Transport)
			if !ok {
				t.Fatalf("transport type = %T, want *http.Transport", transport)
			}

			req, errRequest := http.NewRequest(http.MethodGet, "https://example.com", nil)
			if errRequest != nil {
				t.Fatalf("http.NewRequest returned error: %v", errRequest)
			}

			proxyURL, errProxy := httpTransport.Proxy(req)
			if errProxy != nil {
				t.Fatalf("httpTransport.Proxy returned error: %v", errProxy)
			}
			if proxyURL == nil || proxyURL.String() != tc.wantProxy {
				t.Fatalf("proxy URL = %v, want %s", proxyURL, tc.wantProxy)
			}
		})
	}
}

func TestAuthByIndexDistinguishesSharedAPIKeysAcrossProviders(t *testing.T) {
	t.Parallel()

	manager := coreauth.NewManager(nil, nil, nil)
	geminiAuth := &coreauth.Auth{
		ID:       "gemini:apikey:123",
		Provider: "gemini",
		Attributes: map[string]string{
			"api_key": "shared-key",
		},
	}
	compatAuth := &coreauth.Auth{
		ID:       "openai-compatibility:bohe:456",
		Provider: "bohe",
		Label:    "bohe",
		Attributes: map[string]string{
			"api_key":      "shared-key",
			"compat_name":  "bohe",
			"provider_key": "bohe",
		},
	}

	if _, errRegister := manager.Register(context.Background(), geminiAuth); errRegister != nil {
		t.Fatalf("register gemini auth: %v", errRegister)
	}
	if _, errRegister := manager.Register(context.Background(), compatAuth); errRegister != nil {
		t.Fatalf("register compat auth: %v", errRegister)
	}

	geminiIndex := geminiAuth.EnsureIndex()
	compatIndex := compatAuth.EnsureIndex()
	if geminiIndex == compatIndex {
		t.Fatalf("shared api key produced duplicate auth_index %q", geminiIndex)
	}

	h := &Handler{authManager: manager}

	gotGemini := h.authByIndex(geminiIndex)
	if gotGemini == nil {
		t.Fatal("expected gemini auth by index")
	}
	if gotGemini.ID != geminiAuth.ID {
		t.Fatalf("authByIndex(gemini) returned %q, want %q", gotGemini.ID, geminiAuth.ID)
	}

	gotCompat := h.authByIndex(compatIndex)
	if gotCompat == nil {
		t.Fatal("expected compat auth by index")
	}
	if gotCompat.ID != compatAuth.ID {
		t.Fatalf("authByIndex(compat) returned %q, want %q", gotCompat.ID, compatAuth.ID)
	}
}

type apiCallRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f apiCallRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestIsPublicAPICallIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		address string
		want    bool
	}{
		{address: "8.8.8.8", want: true},
		{address: "2606:4700:4700::1111", want: true},
		{address: "127.0.0.1", want: false},
		{address: "10.0.0.1", want: false},
		{address: "169.254.169.254", want: false},
		{address: "100.64.0.1", want: false},
		{address: "192.168.1.1", want: false},
		{address: "198.18.0.1", want: false},
		{address: "::1", want: false},
		{address: "fe80::1", want: false},
		{address: "fd00::1", want: false},
	}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			t.Parallel()
			addr, errParse := netip.ParseAddr(test.address)
			if errParse != nil {
				t.Fatalf("parse address: %v", errParse)
			}
			if got := isPublicAPICallIP(addr); got != test.want {
				t.Fatalf("isPublicAPICallIP(%s) = %t, want %t", test.address, got, test.want)
			}
		})
	}
}

func TestValidateAPICallURLWithLookup(t *testing.T) {
	t.Parallel()

	lookup := func(_ context.Context, host string) ([]net.IPAddr, error) {
		switch host {
		case "provider.example":
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		case "rebind.example":
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("169.254.169.254")}}, nil
		case "internal.example":
			return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}, nil
		default:
			return nil, fmt.Errorf("unexpected host %q", host)
		}
	}
	allowlist := apiCallHostAllowlist{"provider.example": {}, "internal.example": {}, "rebind.example": {}}

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "allowed public host", raw: "https://provider.example/v1/models", want: true},
		{name: "host outside allowlist", raw: "https://outside.example/v1/models", want: false},
		{name: "DNS private address", raw: "https://internal.example/v1/models", want: false},
		{name: "DNS rebinding response", raw: "https://rebind.example/v1/models", want: false},
		{name: "private literal", raw: "http://169.254.169.254/latest/meta-data", want: false},
		{name: "unsupported scheme", raw: "file:///etc/passwd", want: false},
		{name: "user info", raw: "https://token@provider.example/v1/models", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			parsedURL, errParse := url.Parse(test.raw)
			if errParse != nil {
				t.Fatalf("parse URL: %v", errParse)
			}
			errValidate := validateAPICallURLWithLookup(context.Background(), parsedURL, allowlist, lookup)
			if (errValidate == nil) != test.want {
				t.Fatalf("validateAPICallURLWithLookup(%q) error = %v, want success=%t", test.raw, errValidate, test.want)
			}
		})
	}
}

func TestAPICallAllowedHostsRestrictsOAuthToProviderDomains(t *testing.T) {
	t.Parallel()

	h := &Handler{cfg: &config.Config{CodexKey: []config.CodexKey{{APIKey: "key", BaseURL: "https://configured.example/v1"}}}}
	oauthAuth := &coreauth.Auth{
		Provider: "antigravity",
		Metadata: map[string]any{
			"access_token": "token",
		},
		Attributes: map[string]string{"base_url": "https://attacker.example"},
	}
	oauthAllowlist := h.apiCallAllowedHosts(oauthAuth)
	if !oauthAllowlist.allows("oauth2.googleapis.com") {
		t.Fatal("expected antigravity OAuth allowlist to include oauth2.googleapis.com")
	}
	if oauthAllowlist.allows("attacker.example") {
		t.Fatal("OAuth allowlist must not trust credential base_url")
	}

	apiKeyAuth := &coreauth.Auth{
		Provider:   "codex",
		Attributes: map[string]string{"api_key": "key"},
	}
	apiKeyAllowlist := h.apiCallAllowedHosts(apiKeyAuth)
	if !apiKeyAllowlist.allows("configured.example") {
		t.Fatal("expected API key allowlist to include configured base_url")
	}
	if !apiKeyAllowlist.allows("api.openai.com") {
		t.Fatal("expected Codex allowlist to include api.openai.com")
	}
}

func TestAPICallRedirectValidatorRejectsPrivateTarget(t *testing.T) {
	t.Parallel()

	calls := 0
	client := &http.Client{
		Transport: apiCallRoundTripperFunc(func(_ *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"http://169.254.169.254/latest/meta-data"}},
				Body:       io.NopCloser(strings.NewReader("redirect")),
			}, nil
		}),
		CheckRedirect: apiCallRedirectValidator(nil),
	}

	_, errDo := client.Get("https://provider.example/start")
	if errDo == nil {
		t.Fatal("expected private redirect target to be rejected")
	}
	if calls != 1 {
		t.Fatalf("round trips = %d, want 1", calls)
	}
}

func TestAPICallRejectsPrivateURLAndHostOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "metadata URL",
			payload: `{"method":"GET","url":"http://169.254.169.254/latest/meta-data"}`,
		},
		{
			name:    "Host override",
			payload: `{"method":"GET","url":"https://8.8.8.8/v1/models","header":{"Host":"metadata.google.internal"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			writer := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(writer)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/api-call", strings.NewReader(test.payload))
			ctx.Request.Header.Set("Content-Type", "application/json")

			(&Handler{}).APICall(ctx)
			if writer.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", writer.Code, http.StatusBadRequest, writer.Body.String())
			}
		})
	}
}

func TestDialAPICallAddressPinsResolvedIP(t *testing.T) {
	lookup := func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != "provider.example" {
			t.Fatalf("lookup host=%q", host)
		}
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	var dialed string
	dialErr := fmt.Errorf("stop after capture")
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" {
			t.Fatalf("network=%q", network)
		}
		dialed = address
		return nil, dialErr
	}
	_, errDial := dialAPICallAddress(context.Background(), "tcp", "provider.example:443", lookup, dial)
	if errDial == nil || !strings.Contains(errDial.Error(), dialErr.Error()) {
		t.Fatalf("error=%v", errDial)
	}
	if dialed != "8.8.8.8:443" {
		t.Fatalf("dialed=%q want pinned IP", dialed)
	}
}

func TestAPICallRedirectStripsCrossHostCredentials(t *testing.T) {
	previous, _ := http.NewRequest(http.MethodGet, "https://8.8.8.8/start", nil)
	next, _ := http.NewRequest(http.MethodGet, "https://1.1.1.1/next", nil)
	for _, key := range []string{"Authorization", "Proxy-Authorization", "Cookie", "X-API-Key", "X-Access-Token", "Client-Secret"} {
		next.Header.Set(key, "sensitive")
	}
	next.Header.Set("Accept", "application/json")
	if errValidate := apiCallRedirectValidator(nil)(next, []*http.Request{previous}); errValidate != nil {
		t.Fatal(errValidate)
	}
	for _, key := range []string{"Authorization", "Proxy-Authorization", "Cookie", "X-API-Key", "X-Access-Token", "Client-Secret"} {
		if got := next.Header.Get(key); got != "" {
			t.Fatalf("%s retained: %q", key, got)
		}
	}
	if next.Header.Get("Accept") != "application/json" {
		t.Fatal("non-credential header stripped")
	}
}

func TestReadLimitedAPICallBodyRejectsOverflow(t *testing.T) {
	_, errRead := readLimitedAPICallBody(strings.NewReader("12345"), 4)
	if errRead == nil {
		t.Fatal("expected body limit error")
	}
}

func TestAPICallSafeTransportDisablesConfiguredAndEnvironmentProxies(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://environment-proxy.example:8080")
	t.Setenv("HTTPS_PROXY", "http://environment-proxy.example:8080")
	h := &Handler{cfg: &config.Config{SDKConfig: sdkconfig.SDKConfig{ProxyURL: "http://global-proxy.example:8080"}}}
	transport, ok := h.apiCallSafeTransport(&coreauth.Auth{ProxyURL: "http://credential-proxy.example:8080"}).(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", transport)
	}
	if transport.Proxy != nil {
		t.Fatal("management api-call transport must disable all HTTP(S) proxies")
	}
}

func TestAPICallRedirectRejectsSameHostHTTPSDowngrade(t *testing.T) {
	previous, _ := http.NewRequest(http.MethodGet, "https://8.8.8.8/start", nil)
	next, _ := http.NewRequest(http.MethodGet, "http://8.8.8.8/next", nil)
	next.Header.Set("Authorization", "Bearer secret")
	if errValidate := apiCallRedirectValidator(nil)(next, []*http.Request{previous}); errValidate == nil || !strings.Contains(errValidate.Error(), "downgrade") {
		t.Fatalf("error = %v, want HTTPS downgrade rejection", errValidate)
	}
}

func TestAPICallRedirectStripsCredentialsAcrossPorts(t *testing.T) {
	previous, _ := http.NewRequest(http.MethodGet, "https://8.8.8.8/start", nil)
	next, _ := http.NewRequest(http.MethodGet, "https://8.8.8.8:8443/next", nil)
	for _, key := range []string{"Authorization", "Proxy-Authorization", "Cookie", "X-API-Key", "X-Access-Token", "Client-Secret"} {
		next.Header.Set(key, "sensitive")
	}
	if errValidate := apiCallRedirectValidator(nil)(next, []*http.Request{previous}); errValidate != nil {
		t.Fatal(errValidate)
	}
	for _, key := range []string{"Authorization", "Proxy-Authorization", "Cookie", "X-API-Key", "X-Access-Token", "Client-Secret"} {
		if got := next.Header.Get(key); got != "" {
			t.Fatalf("%s retained across port change: %q", key, got)
		}
	}
}

func TestAPICallOriginUsesEffectivePort(t *testing.T) {
	implicit, _ := url.Parse("https://example.com/path")
	explicit, _ := url.Parse("https://example.com:443/other")
	if got, want := apiCallOrigin(implicit), apiCallOrigin(explicit); got != want {
		t.Fatalf("implicit origin = %q, explicit origin = %q", got, want)
	}
}
