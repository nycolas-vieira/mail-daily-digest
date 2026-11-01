package organizer

import (
	"testing"

	"github.com/nycolas-vieira/mail-daily-digest/internal/config"
)

func TestIsOwn(t *testing.T) {
	cfg := &config.Config{Accounts: []config.Account{
		{Name: "personal", Email: "owner@example.com"},
		{Name: "moonxi", Email: "owner@work.example"},
	}}
	o := New(cfg, nil, nil, nil, false)

	cases := map[string]bool{
		"owner@example.com":    true,
		"OWNER@EXAMPLE.COM":    true,
		"  owner@work.example": true,
		"someone@else.com":     false,
		"":                     false,
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
		"plain@addr.com":                        "plain@addr.com",
		"  Spaced <x@y.com>  ":                  "x@y.com",
	}
	for in, want := range cases {
		if got := extractAddr(in); got != want {
			t.Errorf("extractAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorkPeerAndInvite(t *testing.T) {
	cfg := &config.Config{Accounts: []config.Account{
		{Name: "personal", Email: "owner@gmail.com"},
		{Name: "moonxi", Email: "owner@work.example"},
	}}
	o := New(cfg, nil, nil, nil, false)

	peers := map[string]bool{
		"colleague@work.example": true,
		"COLLEAGUE@WORK.EXAMPLE": true,
		"stranger@gmail.com":     false,
		"news@substack.com":      false,
	}
	for addr, want := range peers {
		if got := o.isWorkPeer(addr); got != want {
			t.Errorf("isWorkPeer(%q) = %v, want %v", addr, got, want)
		}
	}

	invites := map[string]bool{
		"Convite atualizado: Team Weekly - Semanal": true,
		"Invitation: Planning @ Tue":                true,
		"Fwd: Apollo CRM Stage Progression":         false,
		"The Batch: as novidades de IA":             false,
	}
	for subj, want := range invites {
		if got := isCalendarInvite(subj); got != want {
			t.Errorf("isCalendarInvite(%q) = %v, want %v", subj, got, want)
		}
	}
}
