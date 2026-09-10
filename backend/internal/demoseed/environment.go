// Package demoseed provides development-only demo tenant seeding and reset
// for Renovex. Every exported entrypoint here refuses to run outside an
// explicitly-declared development environment — see CheckEnvironmentGuard.
package demoseed

import (
	"fmt"
	"os"
)

// ErrEnvironmentGuardFailed is wrapped by every environment-guard rejection,
// so callers can distinguish "refused to run here" from any other error.
var ErrEnvironmentGuardFailed = fmt.Errorf("demoseed: environment guard failed")

// CheckEnvironmentGuard refuses unless APP_ENV is explicitly present in the
// PROCESS ENVIRONMENT (not a .env file — call this before any .env loading)
// and equals "development" AND RENOVEX_DEMO_TOOL_ENABLED is explicitly
// present and equals "true". Both checks use os.LookupEnv directly — never
// a default-filled config read — because this tool is destructive and must
// be impossible to invoke accidentally outside development (design spec
// §4.1a).
func CheckEnvironmentGuard() error {
	return checkEnvironmentGuardWith(os.LookupEnv)
}

func checkEnvironmentGuardWith(getenv func(string) (string, bool)) error {
	appEnv, appEnvSet := getenv("APP_ENV")
	if !appEnvSet {
		return fmt.Errorf("%w: APP_ENV is not set in the process environment; refusing to run outside an explicit development environment", ErrEnvironmentGuardFailed)
	}
	if appEnv != "development" {
		return fmt.Errorf("%w: APP_ENV=%q, must be exactly \"development\"", ErrEnvironmentGuardFailed, appEnv)
	}

	toolEnabled, toolSet := getenv("RENOVEX_DEMO_TOOL_ENABLED")
	if !toolSet {
		return fmt.Errorf("%w: RENOVEX_DEMO_TOOL_ENABLED is not set; this tool requires explicit opt-in", ErrEnvironmentGuardFailed)
	}
	if toolEnabled != "true" {
		return fmt.Errorf("%w: RENOVEX_DEMO_TOOL_ENABLED=%q, must be exactly \"true\"", ErrEnvironmentGuardFailed, toolEnabled)
	}

	return nil
}
