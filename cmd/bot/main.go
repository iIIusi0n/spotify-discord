package main

import (
	"os"
	"spotify-discord/internal/auth"
)

var (
	oauthAccessURL      = os.Getenv("OAUTH_ACCESS_URL")
	spotifyClientID     = os.Getenv("SPOTIFY_OAUTH_CLIENT_ID")
	spotifyClientSecret = os.Getenv("SPOTIFY_OAUTH_CLIENT_SECRET")
	debugMode           = os.Getenv("DEBUG_MODE") == "true" || os.Getenv("DEBUG_MODE") == "1" || os.Getenv("DEBUG_MODE") == "yes" || os.Getenv("DEBUG_MODE") == "TRUE"
)

func main() {
	authorizer := auth.NewSpotifyAuthorizer(spotifyClientID, spotifyClientSecret, oauthAccessURL, debugMode)
	authorizer.StartOAuthServer()
}
