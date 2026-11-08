package middleware

import (
	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"strings"
)

// SentryMiddleware captures errors and panics to Sentry.
// No-op if Sentry is not initialized (no DSN configured).
func Sentry() gin.HandlerFunc {
	if sentry.CurrentHub().Client() == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return sentrygin.New(sentrygin.Options{Repanic: true})
}

// RedactSentryCredentials also covers custom API-key headers, which are not
// necessarily included in a telemetry provider's default sensitive-header list.
func RedactSentryCredentials(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event.Request != nil {
		for name := range event.Request.Headers {
			if strings.EqualFold(name, "X-API-Key") || strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Cookie") {
				delete(event.Request.Headers, name)
			}
		}
	}
	return event
}
