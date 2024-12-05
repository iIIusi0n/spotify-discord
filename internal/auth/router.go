package auth

import (
	"fmt"
	"net/http"
	"net/url"
	"text/template"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"

	_ "embed"
)

//go:embed index.html
var indexHTML string

func extractListenAddress(accessURL string) string {
	parsedURL, err := url.Parse(accessURL)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("0.0.0.0:%s", parsedURL.Port())
}

func (a *SpotifyAuthorizer) oAuthRouter() *gin.Engine {
	r := gin.Default()

	r.GET("/", func(c *gin.Context) {
		var data struct {
			State string
		}
		if err := a.checkToken(); err != nil {
			data.State = err.Error()
		} else {
			data.State = "logged in"
		}

		t := template.Must(template.New("index").Parse(indexHTML))
		if err := t.Execute(c.Writer, data); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
	})

	r.GET("/index", func(c *gin.Context) {
		c.Redirect(http.StatusPermanentRedirect, "/")
	})

	r.GET("/auth", func(c *gin.Context) {
		url := a.conf.AuthCodeURL("state", oauth2.AccessTypeOffline)
		c.Redirect(http.StatusTemporaryRedirect, url)
	})

	r.GET("/callback", func(c *gin.Context) {
		code := c.Query("code")
		if code == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "code is required"})
			return
		}

		token, err := a.conf.Exchange(c.Request.Context(), code)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to exchange code for token"})
			return
		}

		a.accessToken = token.AccessToken
		a.refreshToken = token.RefreshToken

		c.Redirect(http.StatusTemporaryRedirect, "/")
	})

	if a.debugMode {
		r.GET("/debug", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"accessToken": a.accessToken, "refreshToken": a.refreshToken})
		})
	}

	return r
}

func (a *SpotifyAuthorizer) StartOAuthServer() error {
	listenAddress := extractListenAddress(a.conf.RedirectURL)

	r := a.oAuthRouter()
	return r.Run(listenAddress)
}
