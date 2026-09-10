package identity

import "time"

// User is a person who can authenticate into the platform.
type User struct {
	ID                 string    `bson:"_id,omitempty" json:"id"`
	Email              string    `bson:"email" json:"email"` // normalized: lowercased, trimmed
	PasswordHash       string    `bson:"passwordHash" json:"-"`
	MustChangePassword bool      `bson:"mustChangePassword" json:"mustChangePassword"`
	CreatedAt          time.Time `bson:"createdAt" json:"createdAt"`
}
