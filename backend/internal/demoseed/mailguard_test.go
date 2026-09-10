package demoseed_test

import (
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/platform/config"
)

func TestCheckMailerIsLocalSink(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		port    string
		wantErr bool
	}{
		{"exact localhost:1025", "localhost", "1025", false},
		{"exact loopback:1025", "127.0.0.1", "1025", false},
		{"right host, wrong port", "localhost", "2525", true},
		{"right host, empty port", "localhost", "", true},
		{"external host, right port coincidentally", "smtp.sendgrid.net", "1025", true},
		{"empty host", "", "1025", true},
		{"another loopback form, right port", "0.0.0.0", "1025", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{SMTPHost: tt.host, SMTPPort: tt.port}
			err := demoseed.CheckMailerIsLocalSink(cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckMailerIsLocalSink(%q, %q) error = %v, wantErr %v", tt.host, tt.port, err, tt.wantErr)
			}
		})
	}
}
