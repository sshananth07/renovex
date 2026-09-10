package demoseed

import (
	"fmt"

	"github.com/shananth/renovation-platform/backend/internal/platform/config"
)

// approvedMailpitPort is the EXACT host-mapped SMTP port of this repo's own
// Mailpit service, per docker-compose.yml. Both host AND port must match —
// a matching host on a different port could be a real SMTP relay forwarding
// mail externally, which a host-only check would wrongly accept (design
// spec §4.5).
const approvedMailpitPort = "1025"

var approvedLocalMailSinkHosts = map[string]bool{
	"localhost": true,
	"127.0.0.1": true,
}

// ErrUnsafeMailTransport is returned when the configured SMTP host/port is
// not an exact match for this repo's own Mailpit instance.
var ErrUnsafeMailTransport = fmt.Errorf("demoseed: configured mail transport is not this repository's approved local Mailpit instance")

// CheckMailerIsLocalSink refuses unless cfg.SMTPHost/cfg.SMTPPort is an
// EXACT match for the repository's own local Mailpit instance (host
// localhost/127.0.0.1 AND port 1025 — both required). Call this once, as a
// global preflight, before any tenant data is seeded.
func CheckMailerIsLocalSink(cfg config.Config) error {
	if !approvedLocalMailSinkHosts[cfg.SMTPHost] {
		return fmt.Errorf("%w: SMTP_HOST=%q is not localhost/127.0.0.1", ErrUnsafeMailTransport, cfg.SMTPHost)
	}
	if cfg.SMTPPort != approvedMailpitPort {
		return fmt.Errorf("%w: SMTP_PORT=%q, expected exactly %q (this repo's Mailpit port) — a matching host on a different port could be a real SMTP relay", ErrUnsafeMailTransport, cfg.SMTPPort, approvedMailpitPort)
	}
	return nil
}
