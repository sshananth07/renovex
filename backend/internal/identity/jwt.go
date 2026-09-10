package identity

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AccessTokenClaims are the JWT claims embedded in every access token: a
// snapshot of the user's identity, company, and role at issuance time. Role
// changes take effect on the next refresh, per the tenancy design.
type AccessTokenClaims struct {
	UserID    string `json:"uid"`
	CompanyID string `json:"companyId"`
	Role      string `json:"role"`
	jwt.RegisteredClaims
}

// JWTIssuer issues and verifies HS256-signed access tokens.
type JWTIssuer struct {
	secret []byte
	ttl    time.Duration
}

// NewJWTIssuer constructs a JWTIssuer with the given HMAC secret and access
// token time-to-live.
func NewJWTIssuer(secret []byte, ttl time.Duration) *JWTIssuer {
	return &JWTIssuer{secret: secret, ttl: ttl}
}

// IssueAccessToken returns a signed JWT embedding userID, companyID, and role.
func (j *JWTIssuer) IssueAccessToken(userID, companyID, role string) (string, error) {
	now := time.Now()
	claims := AccessTokenClaims{
		UserID:    userID,
		CompanyID: companyID,
		Role:      role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(j.ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}

// VerifyAccessToken parses and validates tokenString, returning its claims.
// Errors are the sentinel errors from github.com/golang-jwt/jwt/v5 (e.g.
// jwt.ErrTokenExpired, jwt.ErrTokenSignatureInvalid, jwt.ErrTokenMalformed) —
// callers should use errors.Is against those, not string matching.
func (j *JWTIssuer) VerifyAccessToken(tokenString string) (*AccessTokenClaims, error) {
	claims := &AccessTokenClaims{}
	_, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(t *jwt.Token) (any, error) { return j.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}),
	)
	if err != nil {
		return nil, err
	}
	return claims, nil
}
