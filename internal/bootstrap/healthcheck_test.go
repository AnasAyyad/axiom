package bootstrap

import "testing"

func TestValidateHealthURL(t *testing.T) {
	for _, value := range []string{
		"http://127.0.0.1:8080/health/live",
		"http://localhost:9091/health/ready",
		"http://[::1]:8080/health/live",
	} {
		if err := validateHealthURL(value); err != nil {
			t.Fatalf("safe URL rejected: %s: %v", value, err)
		}
	}
	for _, value := range []string{
		"https://127.0.0.1/health/live",
		"http://example.com/health/live",
		"http://127.0.0.1/api/v1/system/status",
		"http://user@127.0.0.1/health/live",
		"http://127.0.0.1/health/live?token=value",
	} {
		if err := validateHealthURL(value); err == nil {
			t.Fatalf("unsafe URL accepted: %s", value)
		}
	}
}
