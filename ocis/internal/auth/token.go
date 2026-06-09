// Package auth extracts the username from an Authorization header.
// Token signature verification is deliberately delegated to oCIS WebDAV:
// every file operation passes the original header through, so oCIS
// rejects invalid credentials itself. We only need the username to
// construct WebDAV paths and temp-file directories.
package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// UsernameFromHeader extracts the username from either:
//   - "Authorization: Bearer <jwt>" — decodes JWT payload, reads preferred_username/sub
//   - "Authorization: Basic <b64>"  — decodes base64(user:pass), returns user
//
// The original header is passed through to oCIS unchanged; oCIS validates it.
func UsernameFromHeader(authHeader string) (string, error) {
	switch {
	case strings.HasPrefix(authHeader, "Bearer "):
		return usernameFromJWT(strings.TrimPrefix(authHeader, "Bearer "))
	case strings.HasPrefix(authHeader, "Basic "):
		return usernameFromBasic(strings.TrimPrefix(authHeader, "Basic "))
	default:
		return "", errors.New("authorization header must be Bearer or Basic")
	}
}

func usernameFromBasic(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", errors.New("cannot base64-decode Basic auth")
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", errors.New("invalid Basic auth format")
	}
	return parts[0], nil
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
