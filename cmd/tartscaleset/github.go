package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const githubAPIVersion = "2022-11-28"

func discoverInstallationID(ctx context.Context, cfg config, privateKey []byte) (int64, error) {
	token, err := githubAppJWT(cfg.AppID, privateKey)
	if err != nil {
		return 0, err
	}

	endpoint := fmt.Sprintf(
		"%s/orgs/%s/installation",
		strings.TrimRight(cfg.GitHubAPIURL, "/"),
		url.PathEscape(cfg.OrgName),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("create GitHub installation request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", "xtool-tart-scale-set")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("find GitHub App installation: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("find GitHub App installation: GitHub returned %s", resp.Status)
	}

	var installation struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&installation); err != nil {
		return 0, fmt.Errorf("decode GitHub App installation: %w", err)
	}
	if installation.ID == 0 {
		return 0, fmt.Errorf("GitHub did not return an installation ID for %s", cfg.OrgName)
	}
	return installation.ID, nil
}

func githubAppJWT(appID string, privateKey []byte) (string, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(privateKey)
	if err != nil {
		return "", fmt.Errorf("parse GitHub App private key: %w", err)
	}
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    appID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("sign GitHub App JWT: %w", err)
	}
	return signed, nil
}
