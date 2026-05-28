package permission

import (
	"testing"

	"github.com/xiaokhkh/sentinel-agent/internal/policy"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		d    policy.Decision
		m    Mode
		want Outcome
	}{
		{policy.Allow, ReadOnly, Run},
		{policy.Confirm, ReadOnly, Ask},
		{policy.Block, ReadOnly, Refuse},
		{policy.Allow, Auto, Run},
		{policy.Confirm, Auto, Run},
		{policy.Block, Auto, Refuse},
		{policy.Allow, Full, Run},
		{policy.Block, Full, Run},
	}
	for _, c := range cases {
		if got := Decide(c.d, c.m); got != c.want {
			t.Errorf("Decide(%s, %s) = %s, want %s", c.d, c.m, got, c.want)
		}
	}
}

func TestParseMode(t *testing.T) {
	if m, ok := ParseMode(""); !ok || m != ReadOnly {
		t.Errorf(`ParseMode("") = %s, %v; want readonly, true`, m, ok)
	}
	if _, ok := ParseMode("nope"); ok {
		t.Error("ParseMode(nope) should fail")
	}
	for _, s := range []string{"readonly", "auto", "full"} {
		if _, ok := ParseMode(s); !ok {
			t.Errorf("ParseMode(%q) should succeed", s)
		}
	}
}
