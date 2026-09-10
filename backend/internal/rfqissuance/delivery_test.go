package rfqissuance_test

import (
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// Invitation delivery attempts (design spec §3.5, §1A.2, §1A.4).
//
// Decision B: the record is persisted BEFORE the mail is sent, and a mail
// failure never rolls back the invitation — it becomes a visible, retryable
// failed attempt instead.

func TestNewDeliveryAttemptStartsPending(t *testing.T) {
	attempt := rfqissuance.NewDeliveryAttempt(rfqissuance.NewDeliveryAttemptInput{
		CompanyID: "company-1", InvitationID: "invitation-1",
		DeliveryOperationID: "op-send-1",
		AccessGeneration:    1,
		Channel:             rfqissuance.DeliveryChannelEmail,
		RecipientEmail:      "sales@supplier.com",
		IssuedRFQVersionID:  "version-1",
		RequestedByUserID:   "user-1",
	})

	if attempt.Status != rfqissuance.DeliveryStatusPending {
		t.Errorf("Status = %q, want pending: the intent is persisted BEFORE the send "+
			"so a crash mid-send is recoverable (§1A.2)", attempt.Status)
	}
	if attempt.SentAt != nil || attempt.DeliveredAt != nil {
		t.Error("a pending attempt has not been sent or delivered")
	}
	if attempt.FailureCode != "" {
		t.Error("a pending attempt carries no failure code")
	}
	if attempt.RequestedAt.IsZero() {
		t.Error("RequestedAt must be stamped")
	}
}

// A copy-link attempt is recorded as SENT immediately: nothing is emailed, the
// contractor takes the link away themselves (§1A.4).
func TestCopyLinkAttemptIsRecordedAsSentWithoutMail(t *testing.T) {
	attempt := rfqissuance.NewDeliveryAttempt(rfqissuance.NewDeliveryAttemptInput{
		CompanyID: "company-1", InvitationID: "invitation-1",
		DeliveryOperationID: "op-copy-1",
		AccessGeneration:    1,
		Channel:             rfqissuance.DeliveryChannelCopyLink,
		RecipientEmail:      "sales@supplier.com",
		IssuedRFQVersionID:  "version-1",
		RequestedByUserID:   "user-1",
	})

	if attempt.Status != rfqissuance.DeliveryStatusSent {
		t.Errorf("Status = %q, want sent: a copy-link performs no mail send", attempt.Status)
	}
	if attempt.SentAt == nil {
		t.Error("SentAt must be stamped on a copy-link attempt")
	}
	if attempt.Channel != rfqissuance.DeliveryChannelCopyLink {
		t.Errorf("Channel = %q, want copy_link", attempt.Channel)
	}
}

// The recipient email is SNAPSHOTTED on the attempt, so the record still says
// who the link was intended for after a later recipient replacement.
func TestDeliveryAttemptSnapshotsTheRecipientAndGeneration(t *testing.T) {
	attempt := rfqissuance.NewDeliveryAttempt(rfqissuance.NewDeliveryAttemptInput{
		CompanyID: "company-1", InvitationID: "invitation-1",
		DeliveryOperationID: "op-send-1",
		AccessGeneration:    3,
		Channel:             rfqissuance.DeliveryChannelEmail,
		RecipientEmail:      "sales@supplier.com",
		IssuedRFQVersionID:  "version-2",
		RequestedByUserID:   "user-1",
	})

	if attempt.RecipientEmail != "sales@supplier.com" {
		t.Errorf("RecipientEmail = %q", attempt.RecipientEmail)
	}
	if attempt.AccessGeneration != 3 {
		t.Errorf("AccessGeneration = %d, want 3: the attempt records WHICH link was sent",
			attempt.AccessGeneration)
	}
	if attempt.IssuedRFQVersionID != "version-2" {
		t.Errorf("IssuedRFQVersionID = %q, want version-2", attempt.IssuedRFQVersionID)
	}
}

// --- bounded failure codes (§1A.2) ---

// Raw provider error text must never be persisted. A bounded code is recorded
// instead, so an SMTP message containing credentials or recipient data cannot
// reach the database.
func TestClassifyDeliveryFailureReturnsABoundedCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want rfqissuance.DeliveryFailureCode
	}{
		{"nil", nil, ""},
		{"any send failure", errSend, rfqissuance.DeliveryFailureCodeSendFailed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rfqissuance.ClassifyDeliveryFailure(tc.err)
			if got != tc.want {
				t.Errorf("code = %q, want %q", got, tc.want)
			}
		})
	}
}

// The code must be a fixed token, never a rendering of the underlying error.
func TestDeliveryFailureCodeDoesNotEmbedProviderText(t *testing.T) {
	code := rfqissuance.ClassifyDeliveryFailure(errSendWithSecret)

	if code == "" {
		t.Fatal("a failure must produce a code")
	}
	if string(code) != string(rfqissuance.DeliveryFailureCodeSendFailed) {
		t.Errorf("code = %q, want the bounded constant. A code derived from the provider "+
			"error could carry credentials or recipient data into the database (§1A.2)",
			code)
	}
}

var (
	errSend           = &stubError{"smtp: connection refused"}
	errSendWithSecret = &stubError{"smtp: 535 auth failed for user admin password hunter2"}
)

type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }

// --- status transitions ---

func TestDeliveryAttemptMarkSentAndMarkFailed(t *testing.T) {
	base := rfqissuance.NewDeliveryAttempt(rfqissuance.NewDeliveryAttemptInput{
		CompanyID: "company-1", InvitationID: "invitation-1",
		DeliveryOperationID: "op-send-1", AccessGeneration: 1,
		Channel: rfqissuance.DeliveryChannelEmail, RecipientEmail: "sales@supplier.com",
		IssuedRFQVersionID: "version-1", RequestedByUserID: "user-1",
	})

	sentAt := time.Now()
	sent := base.MarkSent(sentAt)
	if sent.Status != rfqissuance.DeliveryStatusSent {
		t.Errorf("Status = %q, want sent", sent.Status)
	}
	if sent.SentAt == nil || !sent.SentAt.Equal(sentAt) {
		t.Errorf("SentAt = %v, want %v", sent.SentAt, sentAt)
	}

	failed := base.MarkFailed(rfqissuance.DeliveryFailureCodeSendFailed)
	if failed.Status != rfqissuance.DeliveryStatusFailed {
		t.Errorf("Status = %q, want failed", failed.Status)
	}
	if failed.FailureCode != rfqissuance.DeliveryFailureCodeSendFailed {
		t.Errorf("FailureCode = %q", failed.FailureCode)
	}
	if failed.SentAt != nil {
		t.Error("a failed attempt was never sent")
	}
}
