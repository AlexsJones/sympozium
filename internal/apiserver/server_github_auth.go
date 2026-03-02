package apiserver

// server_github_auth.go — GitHub OAuth device-flow authentication for the
// github-gitops skill.  The flow is:
//
//  1. Client POSTs /api/v1/skills/github-gitops/auth with the desired repo.
//  2. Server requests a device code from GitHub and returns it to the caller.
//  3. User visits https://github.com/login/device and enters the code.
//  4. Server polls GitHub in the background; when the token arrives it is
//     written to the K8s Secret `github-gitops-token` in `sympozium-system`.
//  5. Client polls GET /api/v1/skills/github-gitops/auth/status until the
//     status is "complete" or "expired".
//
// The GitHub OAuth App Client ID is read from the GITHUB_OAUTH_CLIENT_ID
// environment variable on the API server pod.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	githubDeviceCodeURL = "https://github.com/login/device/code"
	githubTokenURL      = "https://github.com/login/oauth/access_token"
	githubTokenSecret   = "github-gitops-token"
	sympoziumNamespace  = "sympozium-system"

	// Scopes needed: repo access for PRs/Issues.
	githubScopes = "repo"
)

// githubAuthState holds in-memory state for an ongoing device flow.
type githubAuthState struct {
	mu           sync.Mutex
	status       string // "pending", "complete", "expired", "error"
	deviceCode   string
	userCode     string
	verifyURI    string
	expiresAt    time.Time
	interval     int // polling interval in seconds required by GitHub
	clientID     string
}

// package-level singleton — only one auth flow is expected at a time.
var githubAuth = &githubAuthState{status: "idle"}

// --------------------------------------------------------------------------
// POST /api/v1/skills/github-gitops/auth
// --------------------------------------------------------------------------

type githubAuthRequest struct {
	// Repo is saved for future reference but not used in the OAuth flow itself.
	Repo string `json:"repo,omitempty"`
}

type githubAuthResponse struct {
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	ExpiresIn       int    `json:"expiresIn"`
	Interval        int    `json:"interval"`
	Status          string `json:"status"`
}

func (s *Server) handleGithubAuth(w http.ResponseWriter, r *http.Request) {
	clientID := os.Getenv("GITHUB_OAUTH_CLIENT_ID")
	if clientID == "" {
		http.Error(w, "GITHUB_OAUTH_CLIENT_ID not configured on the server", http.StatusServiceUnavailable)
		return
	}

	var req githubAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Request a device code from GitHub.
	deviceResp, err := requestGithubDeviceCode(clientID, githubScopes)
	if err != nil {
		s.log.Error(err, "failed to request GitHub device code")
		http.Error(w, fmt.Sprintf("GitHub API error: %v", err), http.StatusBadGateway)
		return
	}

	// Store state.
	githubAuth.mu.Lock()
	githubAuth.status = "pending"
	githubAuth.deviceCode = deviceResp.DeviceCode
	githubAuth.userCode = deviceResp.UserCode
	githubAuth.verifyURI = deviceResp.VerificationURI
	githubAuth.expiresAt = time.Now().Add(time.Duration(deviceResp.ExpiresIn) * time.Second)
	githubAuth.interval = deviceResp.Interval
	githubAuth.clientID = clientID
	githubAuth.mu.Unlock()

	// Start background polling.
	go s.pollGithubToken(r.Context())

	writeJSON(w, githubAuthResponse{
		UserCode:        deviceResp.UserCode,
		VerificationURI: deviceResp.VerificationURI,
		ExpiresIn:       deviceResp.ExpiresIn,
		Interval:        deviceResp.Interval,
		Status:          "pending",
	})
}

// --------------------------------------------------------------------------
// GET /api/v1/skills/github-gitops/auth/status
// --------------------------------------------------------------------------

type githubAuthStatusResponse struct {
	Status string `json:"status"` // "idle", "pending", "complete", "expired", "error"
}

type githubAuthTokenRequest struct {
	Token string `json:"token"`
}

func (s *Server) handleGithubAuthStatus(w http.ResponseWriter, r *http.Request) {
	githubAuth.mu.Lock()
	status := githubAuth.status
	githubAuth.mu.Unlock()

	writeJSON(w, githubAuthStatusResponse{Status: status})
}

// --------------------------------------------------------------------------
// POST /api/v1/skills/github-gitops/auth/token
// --------------------------------------------------------------------------

func (s *Server) handleGithubAuthToken(w http.ResponseWriter, r *http.Request) {
	var req githubAuthTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	token := strings.TrimSpace(req.Token)
	if token == "" {
		http.Error(w, "token is required", http.StatusBadRequest)
		return
	}

	if err := s.writeGithubTokenSecret(token); err != nil {
		http.Error(w, fmt.Sprintf("failed to store token: %v", err), http.StatusInternalServerError)
		return
	}

	githubAuth.mu.Lock()
	githubAuth.status = "complete"
	githubAuth.mu.Unlock()

	writeJSON(w, githubAuthStatusResponse{Status: "complete"})
}

// --------------------------------------------------------------------------
// Background token polling goroutine
// --------------------------------------------------------------------------

func (s *Server) pollGithubToken(ctx context.Context) {
	githubAuth.mu.Lock()
	interval := githubAuth.interval
	if interval < 5 {
		interval = 5
	}
	clientID := githubAuth.clientID
	deviceCode := githubAuth.deviceCode
	expiresAt := githubAuth.expiresAt
	githubAuth.mu.Unlock()

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Now().After(expiresAt) {
				githubAuth.mu.Lock()
				githubAuth.status = "expired"
				githubAuth.mu.Unlock()
				s.log.Info("GitHub device flow expired")
				return
			}

			token, pending, err := pollGithubAccessToken(clientID, deviceCode)
			if err != nil {
				s.log.Error(err, "error polling GitHub token")
				githubAuth.mu.Lock()
				githubAuth.status = "error"
				githubAuth.mu.Unlock()
				return
			}
			if pending {
				// User hasn't approved yet — keep waiting.
				continue
			}

			// We have a token — write it to the K8s Secret.
			if err := s.writeGithubTokenSecret(token); err != nil {
				s.log.Error(err, "failed to write GitHub token secret")
				githubAuth.mu.Lock()
				githubAuth.status = "error"
				githubAuth.mu.Unlock()
				return
			}

			githubAuth.mu.Lock()
			githubAuth.status = "complete"
			githubAuth.mu.Unlock()
			s.log.Info("GitHub authentication complete, token stored", "secret", githubTokenSecret)
			return
		}
	}
}

// writeGithubTokenSecret upserts the github-gitops-token Secret.
func (s *Server) writeGithubTokenSecret(token string) error {
	ctx := context.Background()
	secretKey := types.NamespacedName{Name: githubTokenSecret, Namespace: sympoziumNamespace}

	existing := &corev1.Secret{}
	err := s.client.Get(ctx, secretKey, existing)
	if k8serrors.IsNotFound(err) {
		// Create.
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      githubTokenSecret,
				Namespace: sympoziumNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "sympozium",
					"app.kubernetes.io/component":  "skill-secret",
					"sympozium.ai/skill":           "github-gitops",
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"GH_TOKEN": []byte(token),
			},
		}
		return s.client.Create(ctx, secret)
	}
	if err != nil {
		return fmt.Errorf("get secret: %w", err)
	}

	// Update in place.
	patch := client.MergeFrom(existing.DeepCopy())
	if existing.Data == nil {
		existing.Data = make(map[string][]byte)
	}
	existing.Data["GH_TOKEN"] = []byte(token)
	return s.client.Patch(ctx, existing, patch)
}

// --------------------------------------------------------------------------
// GitHub API helpers
// --------------------------------------------------------------------------

type githubDeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

func requestGithubDeviceCode(clientID, scope string) (*githubDeviceCodeResponse, error) {
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("scope", scope)

	req, err := http.NewRequest(http.MethodPost, githubDeviceCodeURL, bytes.NewBufferString(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", githubDeviceCodeURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub returned %d: %s", resp.StatusCode, body)
	}

	var out githubDeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode device-code response: %w", err)
	}
	if out.Interval == 0 {
		out.Interval = 5
	}
	return &out, nil
}

// pollGithubAccessToken polls the token endpoint once.
// Returns (token, pending, error).  pending==true means the user hasn't
// approved the device yet; the caller should poll again after the interval.
func pollGithubAccessToken(clientID, deviceCode string) (string, bool, error) {
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("device_code", deviceCode)
	params.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	req, err := http.NewRequest(http.MethodPost, githubTokenURL, bytes.NewBufferString(params.Encode()))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("POST %s: %w", githubTokenURL, err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", false, fmt.Errorf("decode token response: %w", err)
	}

	if errCode, ok := result["error"].(string); ok {
		switch errCode {
		case "authorization_pending":
			return "", true, nil
		case "slow_down":
			// GitHub is asking us to slow down — treat as pending.
			return "", true, nil
		case "expired_token":
			return "", false, fmt.Errorf("device code expired")
		case "access_denied":
			return "", false, fmt.Errorf("user denied the authorization request")
		default:
			return "", false, fmt.Errorf("GitHub error: %s", errCode)
		}
	}

	token, ok := result["access_token"].(string)
	if !ok || token == "" {
		return "", false, fmt.Errorf("missing access_token in response")
	}
	return token, false, nil
}


