package rfqissuance

import "time"

// Invitation delivery attempts (design spec §3.5, §1A.2, §1A.4).
//
// The recovery model in one line: the ATTEMPT IS PERSISTED BEFORE THE SEND.
// A crash mid-send leaves a `pending` row that reconciliation can find, and a
// mail failure leaves a visible `failed` row that the contractor can retry —
// neither ever rolls back the invitation itself.

// DeliveryChannel records HOW the link reached the Supplier (§1A.4).
//
// Manual WhatsApp sharing is represented as copy_link: M8 never claims the
// system itself sent a WhatsApp message, only that the contractor took the link.
type DeliveryChannel string

// The supported delivery channels.
const (
	DeliveryChannelEmail    DeliveryChannel = "email"
	DeliveryChannelCopyLink DeliveryChannel = "copy_link"
)

// DeliveryStatus is the lifecycle of one attempt.
type DeliveryStatus string

// The delivery statuses (§3.5).
const (
	DeliveryStatusPending DeliveryStatus = "pending"
	DeliveryStatusSent    DeliveryStatus = "sent"
	DeliveryStatusFailed  DeliveryStatus = "failed"
	// DeliveryStatusDelivered is provider-dependent and is NOT expected to occur
	// against local Mailpit: nothing reports back a delivery receipt. It is
	// reserved so a future provider webhook has a state to write.
	DeliveryStatusDelivered DeliveryStatus = "delivered"
	// DeliveryStatusObsolete marks an attempt whose recipient or access
	// generation changed before it was delivered.
	DeliveryStatusObsolete DeliveryStatus = "obsolete"
)

// DeliveryFailureCode is a BOUNDED failure token.
//
// Bounded is the point: raw provider text is never persisted. An SMTP error
// routinely contains the envelope address, and an auth failure can contain
// credentials — neither belongs in a database row or a log line (§1A.2).
type DeliveryFailureCode string

// The failure codes.
const (
	// DeliveryFailureCodeSendFailed covers every send-time failure. The
	// deliberate coarseness is the safety property: a finer taxonomy derived
	// from provider strings would reintroduce exactly the leak this avoids.
	DeliveryFailureCodeSendFailed DeliveryFailureCode = "send_failed"
)

// ClassifyDeliveryFailure maps a send error to its bounded code.
//
// It deliberately ignores the error's CONTENT. Any non-nil error is
// send_failed; the underlying error is logged at the call site under the
// platform logger's own controls, never persisted here.
func ClassifyDeliveryFailure(err error) DeliveryFailureCode {
	if err == nil {
		return ""
	}
	return DeliveryFailureCodeSendFailed
}

// InvitationDeliveryAttempt is one recorded send or copy.
type InvitationDeliveryAttempt struct {
	ID           string
	CompanyID    string
	InvitationID string
	// DeliveryOperationID is the caller-supplied idempotency key. Reuse must
	// verify the invitation, generation, recipient, target version and channel
	// all match, or it is a different logical send (§10.2).
	DeliveryOperationID string

	// AccessGeneration and RecipientEmail are SNAPSHOTS. After a recipient
	// replacement the invitation moves on, but this record must still say which
	// link went to which address.
	AccessGeneration   int64
	Channel            DeliveryChannel
	RecipientEmail     string
	IssuedRFQVersionID string

	Status      DeliveryStatus
	FailureCode DeliveryFailureCode

	RequestedByUserID string
	RequestedAt       time.Time
	SentAt            *time.Time
	DeliveredAt       *time.Time
	SchemaVersion     int
}

// InvitationDeliveryAttemptSchemaVersion is the current persisted shape.
const InvitationDeliveryAttemptSchemaVersion = 1

// NewDeliveryAttemptInput carries one delivery request.
type NewDeliveryAttemptInput struct {
	CompanyID           string
	InvitationID        string
	DeliveryOperationID string
	AccessGeneration    int64
	Channel             DeliveryChannel
	RecipientEmail      string
	IssuedRFQVersionID  string
	RequestedByUserID   string
}

// NewDeliveryAttempt builds an attempt in its correct initial state.
//
// An email attempt starts PENDING because the mail has not been sent yet — the
// record exists first precisely so an interrupted send is recoverable.
//
// A copy-link attempt starts SENT: there is no asynchronous step to fail. The
// contractor has the link the moment the call returns, so `pending` would
// describe a state that never exists.
func NewDeliveryAttempt(input NewDeliveryAttemptInput) InvitationDeliveryAttempt {
	now := time.Now()

	attempt := InvitationDeliveryAttempt{
		CompanyID:           input.CompanyID,
		InvitationID:        input.InvitationID,
		DeliveryOperationID: input.DeliveryOperationID,
		AccessGeneration:    input.AccessGeneration,
		Channel:             input.Channel,
		RecipientEmail:      input.RecipientEmail,
		IssuedRFQVersionID:  input.IssuedRFQVersionID,
		Status:              DeliveryStatusPending,
		RequestedByUserID:   input.RequestedByUserID,
		RequestedAt:         now,
		SchemaVersion:       InvitationDeliveryAttemptSchemaVersion,
	}

	if input.Channel == DeliveryChannelCopyLink {
		attempt.Status = DeliveryStatusSent
		attempt.SentAt = &now
	}
	return attempt
}

// MarkSent returns the attempt transitioned to sent.
func (a InvitationDeliveryAttempt) MarkSent(sentAt time.Time) InvitationDeliveryAttempt {
	a.Status = DeliveryStatusSent
	a.SentAt = &sentAt
	a.FailureCode = ""
	return a
}

// MarkFailed returns the attempt transitioned to failed under a bounded code.
//
// SentAt stays nil: a failed attempt was never sent, and stamping it would make
// the record claim a delivery that did not happen.
func (a InvitationDeliveryAttempt) MarkFailed(code DeliveryFailureCode) InvitationDeliveryAttempt {
	a.Status = DeliveryStatusFailed
	a.FailureCode = code
	a.SentAt = nil
	return a
}
