package handlers

import "testing"

func TestValidEmail(t *testing.T) {
	for value, want := range map[string]bool{
		"ops@example.com":       true,
		"first.last@shop.co.uk": true,
		"@":                     false,
		"a@":                    false,
		"no-at-sign":            false,
		"Ops <ops@example.com>": false,
		"two@@example.com":      false,
	} {
		if got := validEmail(value); got != want {
			t.Errorf("validEmail(%q) = %v, want %v", value, got, want)
		}
	}
}
