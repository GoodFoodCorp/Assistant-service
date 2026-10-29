package http

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTConfig is what an access token issued by auth-service must match.
type JWTConfig struct {
	Secret   string
	Issuer   string
	Audience string
}

var errNotAccessToken = errors.New("not an access token")

// parseAccessToken verifies an HS256 access token: signature, expiry, issuer,
// audience and typ=access. Refresh tokens (signed with another key and typed
// "refresh"), unsigned tokens and tokens minted for another audience are all
// refused.
func parseAccessToken(raw string, cfg JWTConfig) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (any, error) { return []byte(cfg.Secret), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(cfg.Issuer),
		jwt.WithAudience(cfg.Audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil {
		return nil, err
	}
	if !token.Valid || claims["typ"] != "access" {
		return nil, errNotAccessToken
	}
	return claims, nil
}
