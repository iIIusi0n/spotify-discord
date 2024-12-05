package auth

import (
	"context"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/spotify"
)

type SpotifyAuthorizer struct {
	clientID     string
	clientSecret string

	accessToken  string
	refreshToken string

	conf *oauth2.Config

	debugMode bool
}

func NewSpotifyAuthorizer(clientID, clientSecret, accessURL string, debugMode bool) *SpotifyAuthorizer {
	return &SpotifyAuthorizer{
		clientID:     clientID,
		clientSecret: clientSecret,
		conf: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     spotify.Endpoint,
			RedirectURL:  fmt.Sprintf("%s/callback", accessURL),
			Scopes:       []string{"user-read-playback-state", "user-modify-playback-state", "streaming"},
		},
		debugMode: debugMode,
	}
}

func (a *SpotifyAuthorizer) HttpClient() *http.Client {
	if err := a.checkToken(); err != nil {
		return nil
	}

	return a.conf.Client(context.Background(), &oauth2.Token{
		AccessToken:  a.accessToken,
		RefreshToken: a.refreshToken,
	})
}

func (a *SpotifyAuthorizer) AccessToken() (string, error) {
	if err := a.checkToken(); err != nil {
		return "", fmt.Errorf("failed to validate/refresh token: %w", err)
	}
	return a.accessToken, nil
}

func (a *SpotifyAuthorizer) checkToken() error {
	if a.accessToken == "" {
		return fmt.Errorf("no access token available")
	}

	token := &oauth2.Token{
		AccessToken:  a.accessToken,
		RefreshToken: a.refreshToken,
	}

	if token.Valid() {
		return nil
	}

	tokenSource := a.conf.TokenSource(context.Background(), token)

	newToken, err := tokenSource.Token()
	if err != nil {
		return fmt.Errorf("failed to validate/refresh token: %v", err)
	}

	if newToken.AccessToken != a.accessToken {
		a.accessToken = newToken.AccessToken
	}
	if newToken.RefreshToken != "" && newToken.RefreshToken != a.refreshToken {
		a.refreshToken = newToken.RefreshToken
	}

	return nil
}
