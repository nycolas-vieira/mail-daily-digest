package organizer

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/nycolas-vieira/mail-daily-digest/internal/classify"
	"github.com/nycolas-vieira/mail-daily-digest/internal/config"
	"github.com/nycolas-vieira/mail-daily-digest/internal/gmail"
	"github.com/nycolas-vieira/mail-daily-digest/internal/mailbox"
	"github.com/nycolas-vieira/mail-daily-digest/internal/outlook"
	"github.com/nycolas-vieira/mail-daily-digest/internal/report"
)

const RevisarLabel = "Revisar"

var labelNames = map[string]string{
	"CONTAS":     "Contas",
	"NEWSLETTER": "Newsletter",
	"URGENTE":    "Urgentes",
	"PESSOAL":    "Pessoais",
	"DOCUMENTO":  "Documentos",
	"OUTROS":     "Outros",
}

var archiveCategories = map[string]bool{
	"NEWSLETTER": true,
	"OUTROS":     true,
	"DOCUMENTO":  true,
	"CONTAS":     true,
}

func AllLabelNames() []string {
	names := make([]string, 0, len(labelNames)+1)
	for _, n := range labelNames {
		names = append(names, n)
	}
	names = append(names, RevisarLabel)
	return names
}

type Organizer struct {
	cfg    *config.Config
	bl     *config.Blocklist
	st     *report.State
	cls    *classify.Classifier
	dryRun bool
	own    map[string]bool
}

func New(cfg *config.Config, bl *config.Blocklist, st *report.State, cls *classify.Classifier, dryRun bool) *Organizer {
	own := make(map[string]bool, len(cfg.Accounts))
	for _, a := range cfg.Accounts {
		if e := strings.ToLower(strings.TrimSpace(a.Email)); e != "" {
			own[e] = true
		}
	}
	return &Organizer{cfg: cfg, bl: bl, st: st, cls: cls, dryRun: dryRun, own: own}
}

func (o *Organizer) isOwn(addr string) bool {
	return o.own[strings.ToLower(strings.TrimSpace(addr))]
}

func (o *Organizer) Run(ctx context.Context) {
	for _, acct := range o.cfg.Accounts {
		if err := o.processAccount(ctx, acct); err != nil {
			log.Printf("[%s] FAIL: %v", acct.Name, err)
			o.st.AddError(acct.Name)
		}
	}
}

func (o *Organizer) newClient(acct config.Account) (mailbox.Client, error) {
	switch acct.Provider {
	case "outlook":
		return outlook.NewClient(o.cfg.OutlookClientID, outlook.TokenPath(o.cfg.OutlookTokenDir, acct.Name))
	case "", "gmail":
		return gmail.NewClient(o.cfg.OAuth.ClientID, o.cfg.OAuth.ClientSecret, acct.RefreshToken)
	default:
		return nil, fmt.Errorf("unknown provider %q for account %s", acct.Provider, acct.Name)
	}
}

func (o *Organizer) processAccount(ctx context.Context, acct config.Account) error {
	client, err := o.newClient(acct)
	if err != nil {
		return err
	}

	labelMap, err := client.EnsureLabels(AllLabelNames())
	if err != nil {
		return err
	}

	var exclusions []string
	for _, n := range AllLabelNames() {
		exclusions = append(exclusions, "-label:"+quoteLabel(n))
	}
	query := "in:inbox -in:trash " + strings.Join(exclusions, " ")

	ids, err := client.ListMessageIDs(query, o.cfg.MaxEmailsPerRun)
	if err != nil {
		return err
	}
	log.Printf("[%s] %d candidate(s)", acct.Name, len(ids))
	if len(ids) == 0 {
		return nil
	}

	var toClassify []mailbox.Message
	for _, id := range ids {
		m := client.GetMessage(id)
		if m == nil {
			continue
		}
		from := strings.ToLower(m.Header("From"))
		switch {
		case o.isOwn(extractAddr(from)):
			if id := labelMap[labelNames["PESSOAL"]]; id != "" {
				o.act(client, "label:"+labelNames["PESSOAL"], m, acct, false, id)
			}
			log.Printf("[%s] self-mail (skip LLM) %s", acct.Name, extractAddr(from))
		case o.bl.IsPriority(from):
			o.act(client, "label:"+labelNames["URGENTE"], m, acct, false, labelMap[labelNames["URGENTE"]])
			o.st.AddAlert(fmt.Sprintf("%s: email de %s", acct.Name, extractAddr(from)))
			log.Printf("[%s] priority->URGENTE %s", acct.Name, from)
		case o.bl.IsHard(from):
			o.act(client, "trash", m, acct, true, "")
			log.Printf("[%s] hard-trash %s", acct.Name, from)
		case o.bl.IsSoft(from):
			o.act(client, "revisar", m, acct, false, labelMap[RevisarLabel])
			o.st.AddRevisarSender(extractAddr(from))
			log.Printf("[%s] soft-review %s", acct.Name, from)
		default:
			toClassify = append(toClassify, m)
		}
	}

	for _, m := range toClassify {
		d, err := o.cls.Classify(ctx, acct.Name, acct.Email, m)
		if err != nil {
			log.Printf("[%s] classify %s failed: %v", acct.Name, m.ID(), err)
			o.st.AddError(acct.Name)
			continue
		}
		o.applyDecision(client, m, acct, d, labelMap)
	}

	log.Printf("[%s] done", acct.Name)
	return nil
}

func (o *Organizer) applyDecision(client mailbox.Client, m mailbox.Message, acct config.Account, d classify.Decision, labelMap map[string]string) {
	log.Printf("[%s] %-10s %-45s | %s | %s", acct.Name, d.Category, truncate(extractAddr(m.Header("From")), 45), truncate(d.Reason, 60), truncate(m.Header("Subject"), 50))
	if d.Category == "LIXO" {
		o.act(client, "trash", m, acct, false, "")
		o.learnSoft(acct, m.Header("From"))
	} else {
		cat := d.Category
		labelName, ok := labelNames[cat]
		if !ok {
			cat, labelName = "OUTROS", labelNames["OUTROS"]
		}
		o.act(client, "label:"+labelName, m, acct, false, labelMap[labelName])
	}
	if d.Alert != "" {
		o.st.AddAlert(fmt.Sprintf("%s: %s", acct.Name, d.Alert))
	}
}

func (o *Organizer) learnSoft(acct config.Account, from string) {
	if o.isOwn(extractAddr(from)) {
		return
	}
	if o.dryRun {
		if addr := o.bl.PreviewLearnSoft(from); addr != "" {
			o.st.AddSoftLearned(addr)
			log.Printf("[%s] would learn SOFT %s", acct.Name, addr)
		}
		return
	}
	learned, err := o.bl.LearnSoft(from)
	if err != nil {
		log.Printf("[%s] learn-soft failed: %v", acct.Name, err)
		return
	}
	if learned != "" {
		o.st.AddSoftLearned(learned)
		log.Printf("[%s] learned SOFT %s", acct.Name, learned)
	}
}

func (o *Organizer) act(client mailbox.Client, kind string, m mailbox.Message, acct config.Account, hard bool, labelID string) {
	switch {
	case kind == "trash":
		if !o.dryRun {
			if err := client.Trash(m.ID()); err != nil {
				log.Printf("[%s] trash %s: %v", acct.Name, m.ID(), err)
				return
			}
		}
		o.st.AddTrashed(acct.Name, hard)
	case kind == "revisar":
		if labelID == "" {
			return
		}
		if !o.dryRun {
			if err := client.ApplyLabel(m.ID(), labelID, false); err != nil {
				log.Printf("[%s] revisar %s: %v", acct.Name, m.ID(), err)
				return
			}
		}
		o.st.AddLabeled(RevisarLabel)
	case strings.HasPrefix(kind, "label:"):
		labelName := strings.TrimPrefix(kind, "label:")
		if labelID == "" {
			return
		}
		archive := false
		for cat, name := range labelNames {
			if name == labelName && archiveCategories[cat] {
				archive = true
				break
			}
		}
		if !o.dryRun {
			if err := client.ApplyLabel(m.ID(), labelID, archive); err != nil {
				log.Printf("[%s] label %s: %v", acct.Name, m.ID(), err)
				return
			}
		}
		o.st.AddLabeled(labelName)
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func extractAddr(fromHeader string) string {
	low := strings.ToLower(strings.TrimSpace(fromHeader))
	if i := strings.IndexByte(low, '<'); i >= 0 {
		if j := strings.IndexByte(low[i:], '>'); j > 0 {
			return strings.TrimSpace(low[i+1 : i+j])
		}
	}
	return low
}

func quoteLabel(name string) string {
	if strings.Contains(name, " ") {
		return `"` + name + `"`
	}
	return name
}
