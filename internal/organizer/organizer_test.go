package organizer

import (
	"testing"

	"github.com/nycolas-vieira/gmail-daily-digest/internal/config"
)

func TestIsOwn(t *testing.T) {
	cfg := &config.Config{Accounts: []config.Account{
		{Name: "personal", Email: "owner@example.com"},
		{Name: "moonxi", Email: "owner@work.example"},
	}}
	o := New(cfg, nil, nil, nil, false)

	cases := map[string]bool{
		"owner@example.com":    true,
		"OWNER@EXAMPLE.COM":    true, // case-insensitive
		"  owner@work.example": true, // trimmed
		"someone@else.com":        false,
		"":                        false,
	}
	for addr, want := range cases {
		if got := o.isOwn(addr); got != want {
			t.Errorf("isOwn(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestExtractAddr(t *testing.T) {
	cases := map[string]string{
		`"Gmail Organizer" <owner@example.com>`: "owner@example.com",
		"plain@addr.com":       "plain@addr.com",
		"  Spaced <x@y.com>  ": "x@y.com",
	}
	for in, want := range cases {
		if got := extractAddr(in); got != want {
			t.Errorf("extractAddr(%q) = %q, want %q", in, got, want)
		}
	}
}
