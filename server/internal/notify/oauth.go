package notify

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func newOAuthClient(ctx context.Context, c *google.Credentials) *http.Client {
	return oauth2.NewClient(ctx, c.TokenSource)
}
