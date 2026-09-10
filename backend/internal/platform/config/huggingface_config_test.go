package config

import "testing"

func TestLoadHuggingFaceConfig_RequiredTogether(t *testing.T) {
	t.Setenv("HUGGINGFACE_SPACE_URL", "https://example.hf.space")
	t.Setenv("HUGGINGFACE_TOKEN", "")
	t.Setenv("HUNYUAN_PROVIDER_TIMEOUT", "")
	_, _, _, err := loadHuggingFaceConfig()
	if err == nil {
		t.Error("expected an error when only HUGGINGFACE_SPACE_URL is set")
	}
}

func TestLoadHuggingFaceConfig_TokenWithoutURLRejected(t *testing.T) {
	t.Setenv("HUGGINGFACE_SPACE_URL", "")
	t.Setenv("HUGGINGFACE_TOKEN", "sometoken")
	_, _, _, err := loadHuggingFaceConfig()
	if err == nil {
		t.Error("expected an error when only HUGGINGFACE_TOKEN is set")
	}
}

func TestLoadHuggingFaceConfig_BothUnsetIsValid(t *testing.T) {
	t.Setenv("HUGGINGFACE_SPACE_URL", "")
	t.Setenv("HUGGINGFACE_TOKEN", "")
	url, token, timeout, err := loadHuggingFaceConfig()
	if err != nil || url != "" || token != "" {
		t.Errorf("expected both-unset to be valid with empty values, got url=%q token=%q err=%v", url, token, err)
	}
	if timeout != defaultHunyuanProviderTimeout {
		t.Errorf("expected the default timeout when unset, got %v", timeout)
	}
}

func TestLoadHuggingFaceConfig_BothSetIsValid(t *testing.T) {
	t.Setenv("HUGGINGFACE_SPACE_URL", "https://example.hf.space")
	t.Setenv("HUGGINGFACE_TOKEN", "sometoken")
	t.Setenv("HUNYUAN_PROVIDER_TIMEOUT", "")
	url, token, _, err := loadHuggingFaceConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://example.hf.space" || token != "sometoken" {
		t.Errorf("expected url/token to round-trip, got url=%q token=%q", url, token)
	}
}

func TestLoadHuggingFaceConfig_InvalidTimeoutRejected(t *testing.T) {
	t.Setenv("HUGGINGFACE_SPACE_URL", "https://example.hf.space")
	t.Setenv("HUGGINGFACE_TOKEN", "sometoken")
	t.Setenv("HUNYUAN_PROVIDER_TIMEOUT", "not-a-duration")
	_, _, _, err := loadHuggingFaceConfig()
	if err == nil {
		t.Error("expected an error for an invalid HUNYUAN_PROVIDER_TIMEOUT")
	}
}
