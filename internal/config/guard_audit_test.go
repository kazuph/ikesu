package config

import (
	"testing"

	"github.com/kazuph/ikesu/internal/guard"
)

// [guard] audit_weekly: 既定は月曜 10:00、"off" で止める。
func TestGuardAuditWeeklySetting(t *testing.T) {
	for toml, want := range map[string]guard.WeeklySpec{
		"":                                  {Day: 1, Hour: 10},
		"[guard]\naudit_weekly = \"off\"\n": {Off: true},
		"[guard]\naudit_weekly = \"fri 17:30\"\n": {Day: 5, Hour: 17, Min: 30},
	} {
		c, err := Decode([]byte(toml))
		if err != nil {
			t.Fatalf("%q: %v", toml, err)
		}
		got, err := guard.ParseWeekly(c.Guard.AuditWeekly)
		if err != nil || got != want {
			t.Errorf("%q: %+v %v want %+v", toml, got, err, want)
		}
	}
}
