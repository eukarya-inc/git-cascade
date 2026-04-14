package github

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v84/github"
)

// AuthMethod represents the authentication strategy.
type AuthMethod int

const (
	AuthPAT AuthMethod = iota
	AuthGitHubApp
)

// Credentials holds the information needed to authenticate with GitHub.
type Credentials struct {
	Method AuthMethod

	// PAT authentication
	Token string

	// GitHub App authentication
	AppID          int64
	InstallationID int64
	PrivateKeyPath string
}

// NewClient creates an authenticated GitHub client from the given credentials.
func NewClient(creds Credentials) (*github.Client, error) {
	switch creds.Method {
	case AuthPAT:
		return newPATClient(creds.Token)
	case AuthGitHubApp:
		return newAppClient(creds.AppID, creds.InstallationID, creds.PrivateKeyPath)
	default:
		return nil, fmt.Errorf("unknown auth method: %d", creds.Method)
	}
}

func newPATClient(token string) (*github.Client, error) {
	if token == "" {
		return nil, fmt.Errorf("PAT token is required")
	}
	return github.NewClient(nil).WithAuthToken(token), nil
}

func newAppClient(appID, installationID int64, privateKeyPath string) (*github.Client, error) {
	if appID == 0 || installationID == 0 || privateKeyPath == "" {
		return nil, fmt.Errorf("app-id, installation-id, and private-key-path are all required for GitHub App auth")
	}

	key, err := loadPrivateKey(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("loading private key: %w", err)
	}

	transport := &appTransport{
		appID:          appID,
		installationID: installationID,
		key:            key,
		base:           http.DefaultTransport,
	}

	httpClient := &http.Client{Transport: transport}
	return github.NewClient(httpClient), nil
}

func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 as fallback
		parsed, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("parsing private key: %w (also tried PKCS8: %w)", err, err2)
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA")
		}
		return rsaKey, nil
	}
	return key, nil
}

// CredentialsFromEnv builds credentials from environment variables.
// It checks for GitHub App credentials first, then falls back to PAT.
func CredentialsFromEnv() (Credentials, error) {
	// Check for GitHub App credentials
	appIDStr := os.Getenv("GIT_CASCADE_APP_ID")
	installationIDStr := os.Getenv("GIT_CASCADE_INSTALLATION_ID")
	privateKeyPath := os.Getenv("GIT_CASCADE_PRIVATE_KEY_PATH")

	if appIDStr != "" && installationIDStr != "" && privateKeyPath != "" {
		appID, err := strconv.ParseInt(appIDStr, 10, 64)
		if err != nil {
			return Credentials{}, fmt.Errorf("parsing GIT_CASCADE_APP_ID: %w", err)
		}
		installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
		if err != nil {
			return Credentials{}, fmt.Errorf("parsing GIT_CASCADE_INSTALLATION_ID: %w", err)
		}
		return Credentials{
			Method:         AuthGitHubApp,
			AppID:          appID,
			InstallationID: installationID,
			PrivateKeyPath: privateKeyPath,
		}, nil
	}

	// Fall back to PAT
	token := os.Getenv("GIT_CASCADE_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token != "" {
		return Credentials{
			Method: AuthPAT,
			Token:  token,
		}, nil
	}

	return Credentials{}, fmt.Errorf("no GitHub credentials found; set GIT_CASCADE_TOKEN or GitHub App env vars")
}

// appTransport implements http.RoundTripper for GitHub App authentication.
// It generates a JWT, exchanges it for an installation token, and caches
// the installation token until it expires.
type appTransport struct {
	appID          int64
	installationID int64
	key            *rsa.PrivateKey
	base           http.RoundTripper

	// cached installation token
	token     string
	expiresAt time.Time
}

func (t *appTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.getInstallationToken(req.Context())
	if err != nil {
		return nil, err
	}
	req2 := req.Clone(req.Context())
	req2.Header.Set("Authorization", "token "+token)
	return t.base.RoundTrip(req2)
}

func (t *appTransport) getInstallationToken(ctx context.Context) (string, error) {
	if t.token != "" && time.Now().Before(t.expiresAt.Add(-time.Minute)) {
		return t.token, nil
	}

	jwtToken, err := t.generateJWT()
	if err != nil {
		return "", err
	}

	client := github.NewClient(&http.Client{Transport: t.base}).WithAuthToken(jwtToken)
	tok, _, err := client.Apps.CreateInstallationToken(
		ctx, t.installationID, nil,
	)
	if err != nil {
		return "", fmt.Errorf("creating installation token: %w", err)
	}

	t.token = tok.GetToken()
	t.expiresAt = tok.GetExpiresAt().Time
	return t.token, nil
}

func (t *appTransport) generateJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
		Issuer:    strconv.FormatInt(t.appID, 10),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(t.key)
	if err != nil {
		return "", fmt.Errorf("signing JWT: %w", err)
	}
	return signed, nil
}
