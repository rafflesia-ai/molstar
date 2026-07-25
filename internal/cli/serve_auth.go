package cli

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
	"time"
)

func authHTTPHandler(flags *serveFlags, next http.Handler) http.Handler {
	token := strings.TrimSpace(flags.authToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("MOLSTAR_AUTH_TOKEN"))
	}
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/ready" {
			next.ServeHTTP(w, r)
			return
		}
		if !constantTimeEqual(requestToken(r), token) {
			authErr := markError(kindSecurity, http.ErrNoCookie)
			body := newErrorBody(authErr)
			body.Message = "missing or invalid authorization token"
			writeHTTPJSON(w, http.StatusUnauthorized, errorReport{
				OK:        false,
				Command:   "serve",
				Error:     body,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// constantTimeEqual compares two tokens without leaking their length
// relationship through timing. subtle.ConstantTimeCompare returns 0 when the
// lengths differ, so this reports a mismatch in that case too.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func requestToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[len("Bearer "):])
	}
	return strings.TrimSpace(r.Header.Get("X-Molstar-Token"))
}
