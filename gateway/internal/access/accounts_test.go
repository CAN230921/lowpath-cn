package access

import (
	"strings"
	"testing"
)

func TestPasswordLength(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
		valid    bool
	}{
		{"seven ASCII characters", strings.Repeat("a", 7), false},
		{"eight ASCII characters", strings.Repeat("a", 8), true},
		{"seven multibyte characters", strings.Repeat("字", 7), false},
		{"eight multibyte characters", strings.Repeat("字", 8), true},
		{"at byte limit", strings.Repeat("界", 85) + "a", true},
		{"over byte limit", strings.Repeat("界", 86), false},
		{"invalid UTF-8", strings.Repeat("a", 8) + "\xff", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidPassword([]byte(tc.password)); got != tc.valid {
				t.Errorf("ValidPassword = %v, want %v", got, tc.valid)
			}
		})
	}
}
