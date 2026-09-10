package identity

import "time"

// AuthSession tracks one issued refresh token's lifecycle: creation, rotation,
// expiry, and revocation. identity owns the auth_sessions collection exclusively.
type AuthSession struct {
	ID               string     `bson:"_id,omitempty" json:"id"`
	UserID           string     `bson:"userId" json:"userId"`
	RefreshTokenHash string     `bson:"refreshTokenHash" json:"-"`
	CreatedAt        time.Time  `bson:"createdAt" json:"createdAt"`
	ExpiresAt        time.Time  `bson:"expiresAt" json:"expiresAt"`
	LastUsedAt       time.Time  `bson:"lastUsedAt" json:"lastUsedAt"`
	RevokedAt        *time.Time `bson:"revokedAt,omitempty" json:"revokedAt,omitempty"`
}

// IsActive reports whether the session is neither expired nor revoked as of now.
func (s AuthSession) IsActive(now time.Time) bool {
	if s.RevokedAt != nil {
		return false
	}
	return now.Before(s.ExpiresAt)
}
