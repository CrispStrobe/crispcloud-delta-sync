// Package auth extracts the username from an OIDC Bearer token.
// Token signature verification is deliberately delegated to oCIS WebDAV:
// every file operation passes the original Bearer token through, so oCIS
// rejects invalid tokens itself. We only need the username to construct
// WebDAV paths and temp-file directories.
package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// UsernameFromHeader parses an "Authorization: Bearer <jwt>" header and
// returns the value of the "preferred_username" claim (falling back to "sub").
func UsernameFromHeader(authHeader string) (string, error) {
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		return "", errors.New("authorization header is not a Bearer token")
	}
	return usernameFromJWT(token)
}

func usernameFromJWT(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("not a three-part JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("cannot base64-decode JWT payload")
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errors.New("cannot parse JWT claims")
	}
	for _, key := range []string{"preferred_username", "sub"} {
		if u, ok := claims[key].(string); ok && u != "" {
			return u, nil
		}
	}
	return "", errors.New("JWT has no username claim (preferred_username or sub)")
}
