package redact

import "testing"

func TestIsDenied(t *testing.T) {
	r := New([]string{".env*", "*.pem", "secrets/**", "id_rsa*"}, false)
	cases := map[string]bool{
		".env":                  true,
		".env.local":            true,
		"config/.env":           true,
		"cert.pem":              true,
		"deep/nested/key.pem":   true,
		"secrets/prod/db.yaml":  true,
		"secrets/db.yaml":       true,
		"id_rsa":                true,
		"src/main.go":           false,
		"env.go":                false,
		"README.md":             false,
	}
	for path, want := range cases {
		if got := r.IsDenied(path); got != want {
			t.Errorf("IsDenied(%q)=%v want %v", path, got, want)
		}
	}
}

func TestMaskLine(t *testing.T) {
	r := New(nil, true)
	secret := `api_key: "abcdef123456"`
	masked, did := r.MaskLine(secret)
	if !did {
		t.Errorf("expected masking for %q", secret)
	}
	if masked == secret {
		t.Errorf("line not masked: %q", masked)
	}

	clean := "func main() {}"
	if _, did := r.MaskLine(clean); did {
		t.Errorf("clean line should not be masked")
	}
}

func TestMaskLineDisabled(t *testing.T) {
	r := New(nil, false)
	if _, did := r.MaskLine(`api_key="abcdef123456"`); did {
		t.Errorf("masking should be off when scanRegex=false")
	}
}
