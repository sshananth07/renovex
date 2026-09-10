package demoseed

import "testing"

func TestCheckEnvironmentGuard(t *testing.T) {
	tests := []struct {
		name        string
		appEnv      string
		appEnvSet   bool
		toolEnabled string
		toolSet     bool
		wantErr     bool
	}{
		{"missing APP_ENV", "", false, "true", true, true},
		{"wrong APP_ENV", "production", true, "true", true, true},
		{"missing tool flag", "development", true, "", false, true},
		{"tool flag not true", "development", true, "false", true, true},
		{"tool flag empty string", "development", true, "", true, true},
		{"both correct", "development", true, "true", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) (string, bool) {
				switch key {
				case "APP_ENV":
					return tt.appEnv, tt.appEnvSet
				case "RENOVEX_DEMO_TOOL_ENABLED":
					return tt.toolEnabled, tt.toolSet
				}
				return "", false
			}
			err := checkEnvironmentGuardWith(getenv)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkEnvironmentGuardWith() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
