package deliver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/nycolas-vieira/mail-daily-digest/internal/gmail"
)

type Sender interface {
	Send(from, to, subject, htmlBody string) error
}

type Summary struct {
	PeriodStart    time.Time
	PeriodEnd      time.Time
	TrashedTotal   int
	TrashedHard    int
	LabeledTotal   int
	RevisarCount   int
	RevisarSenders []string
	SoftListTotal  int
	HardListTotal  int
	Alerts         []string
}

const promoteHint = "mail-daily-digest -config ~/repos/mail-daily-digest/config.json -promote <addr>"

func Email(s Sender, from, to string, sum Summary) error {
	subject := fmt.Sprintf("Gmail Organizer - %s", sum.PeriodEnd.Format("02/01 15:04"))
	return s.Send(from, to, subject, emailHTML(sum))
}

func emailHTML(sum Summary) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w(`<div style="font-family:-apple-system,Segoe UI,Roboto,sans-serif;color:#1a1a1a;max-width:640px">`)
	w(`<h2 style="margin:0 0 4px">Gmail Organizer</h2>`)
	w(`<p style="color:#666;margin:0 0 16px">Período: %s -> %s</p>`,
		html.EscapeString(sum.PeriodStart.Format("02/01 15:04")),
		html.EscapeString(sum.PeriodEnd.Format("02/01 15:04")))

	geminiLixo := sum.TrashedTotal - sum.TrashedHard
	if geminiLixo < 0 {
		geminiLixo = 0
	}
	w(`<ul style="line-height:1.6">`)
	w(`<li><b>Apagados:</b> %d (HARD silencioso %d, LLM LIXO %d)</li>`, sum.TrashedTotal, sum.TrashedHard, geminiLixo)
	w(`<li><b>Catalogados:</b> %d</li>`, sum.LabeledTotal)
	w(`<li><b>Marcados "Revisar":</b> %d</li>`, sum.RevisarCount)
	w(`<li><b>Listas:</b> HARD %d, SOFT %d</li>`, sum.HardListTotal, sum.SoftListTotal)
	w(`</ul>`)

	cands := dedupe(sum.RevisarSenders)
	if len(cands) > 0 {
		w(`<h3 style="margin:18px 0 6px">Candidatos a promover (SOFT -> HARD)</h3>`)
		w(`<p style="color:#666;margin:0 0 8px">Esses senders bateram em "Revisar" neste período. Se forem lixo confirmado, promova pra HARD (auto-trash, sem label):</p>`)
		w(`<ul style="line-height:1.6">`)
		for _, c := range cands {
			w(`<li><code>%s</code></li>`, html.EscapeString(c))
		}
		w(`</ul>`)
		w(`<pre style="background:#f4f4f4;padding:10px;border-radius:4px;overflow:auto">%s</pre>`, html.EscapeString(promoteHint))
	}

	if len(sum.Alerts) > 0 {
		w(`<h3 style="margin:18px 0 6px">Alertas</h3><ul style="line-height:1.6">`)
		for _, a := range sum.Alerts {
			w(`<li>%s</li>`, html.EscapeString(a))
		}
		w(`</ul>`)
	}
	w(`</div>`)
	return b.String()
}

func EchoPush(webhookURL, secret string, sum Summary) error {
	url := strings.TrimRight(webhookURL, "/") + "/" + secret
	body := map[string]any{
		"period_start":    sum.PeriodStart.Format(time.RFC3339),
		"period_end":      sum.PeriodEnd.Format(time.RFC3339),
		"trashed_total":   sum.TrashedTotal,
		"trashed_hard":    sum.TrashedHard,
		"labeled_total":   sum.LabeledTotal,
		"revisar_count":   sum.RevisarCount,
		"revisar_senders": dedupe(sum.RevisarSenders),
		"soft_total":      sum.SoftListTotal,
		"hard_total":      sum.HardListTotal,
		"alerts":          sum.Alerts,
		"promote_hint":    promoteHint,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	hc := &http.Client{Timeout: 30 * time.Second}
	resp, err := hc.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("echo push: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("echo push: HTTP %d", resp.StatusCode)
	}
	return nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

var _ Sender = (*gmail.Client)(nil)
