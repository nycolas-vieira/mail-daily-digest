package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/nycolas-vieira/mail-daily-digest/internal/classify"
	"github.com/nycolas-vieira/mail-daily-digest/internal/config"
	"github.com/nycolas-vieira/mail-daily-digest/internal/deliver"
	"github.com/nycolas-vieira/mail-daily-digest/internal/gmail"
	"github.com/nycolas-vieira/mail-daily-digest/internal/organizer"
	"github.com/nycolas-vieira/mail-daily-digest/internal/outlook"
	"github.com/nycolas-vieira/mail-daily-digest/internal/report"
)

func main() {
	var (
		configPath  = flag.String("config", "config.json", "path to config.json")
		dryRun      = flag.Bool("dry-run", false, "classify and log actions but change nothing and persist nothing")
		doReport    = flag.Bool("report", false, "render the period digest to report_dir, deliver it, and reset counters")
		doReset     = flag.Bool("reset", false, "clear counters and start a fresh period")
		promote     = flag.String("promote", "", "move a sender from the SOFT tier to HARD (auto-trash) and exit")
		outlookAuth = flag.String("outlook-auth", "", "device-code authenticate an outlook account (by config name) and exit")
	)
	flag.Parse()

	log.SetFlags(log.Ltime)

	if err := run(*configPath, *dryRun, *doReport, *doReset, *promote, *outlookAuth); err != nil {
		log.Fatalf("FATAL: %v", err)
	}
}

func run(configPath string, dryRun, doReport, doReset bool, promote, outlookAuth string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	now := time.Now()
	st, err := report.LoadState(cfg.StatePath, now)
	if err != nil {
		return err
	}

	switch {
	case outlookAuth != "":
		return authOutlook(cfg, outlookAuth)

	case promote != "":
		return promoteSender(cfg, promote)

	case doReset:
		if err := st.Reset(now); err != nil {
			return err
		}
		fmt.Println("state reset; new period starts", now.Format("2006-01-02 15:04"))
		return nil

	case doReport:
		return generateReport(cfg, st, now)

	default:
		return organize(cfg, st, dryRun)
	}
}

func authOutlook(cfg *config.Config, name string) error {
	acct := cfg.AccountByName(name)
	if acct == nil {
		return fmt.Errorf("account %q not found in config", name)
	}
	if acct.Provider != "outlook" {
		return fmt.Errorf("account %q is provider %q, not outlook", name, acct.Provider)
	}
	if cfg.OutlookClientID == "" {
		return fmt.Errorf("outlook_client_id not set in config")
	}
	return outlook.DeviceCode(cfg.OutlookClientID, outlook.TokenPath(cfg.OutlookTokenDir, name))
}

func promoteSender(cfg *config.Config, addr string) error {
	bl, err := config.LoadBlocklist(cfg.BlocklistPath)
	if err != nil {
		return err
	}
	changed, err := bl.PromoteToHard(addr)
	if err != nil {
		return err
	}
	if changed {
		fmt.Printf("promoted to HARD (auto-trash): %s\n", addr)
	} else {
		fmt.Printf("no change: %s already HARD or not found in SOFT\n", addr)
	}
	return nil
}

func organize(cfg *config.Config, st *report.State, dryRun bool) error {
	bl, err := config.LoadBlocklist(cfg.BlocklistPath)
	if err != nil {
		return err
	}

	cls := classify.New(cfg.Ollama.Endpoint, cfg.Ollama.Model, cfg.MaxBodyChars)
	ctx := context.Background()
	if err := cls.Ping(ctx); err != nil {
		return err
	}

	if dryRun {
		log.Printf("DRY-RUN: no Gmail action taken, no state/blocklist written")
	}

	org := organizer.New(cfg, bl, st, cls, dryRun)
	org.Run(ctx)

	if dryRun {
		log.Printf("DRY-RUN complete (state not saved)")
		return nil
	}
	if err := st.Save(); err != nil {
		return err
	}
	log.Printf("done: trashed=%d labeled=%d (state %s)", st.TrashedTotal, st.LabeledTotal, cfg.StatePath)
	return nil
}

func generateReport(cfg *config.Config, st *report.State, now time.Time) error {
	bl, err := config.LoadBlocklist(cfg.BlocklistPath)
	if err != nil {
		return err
	}
	hard, soft := bl.Counts()

	opts := report.RenderOpts{
		Now:           now,
		RevisarLabel:  organizer.RevisarLabel,
		HardListCount: hard,
		SoftListCount: soft,
	}
	path, err := st.SaveReport(cfg.ReportDir, opts)
	if err != nil {
		return err
	}
	fmt.Println("report saved:", path)

	sum := deliver.Summary{
		PeriodStart:    st.PeriodStart,
		PeriodEnd:      now,
		TrashedTotal:   st.TrashedTotal,
		TrashedHard:    st.LixoHard,
		LabeledTotal:   st.LabeledTotal,
		RevisarCount:   st.PerLabel[organizer.RevisarLabel],
		RevisarSenders: st.RevisarSenders,
		SoftListTotal:  soft,
		HardListTotal:  hard,
		Alerts:         st.Alerts,
	}
	deliverReport(cfg, sum)

	if err := st.Reset(now); err != nil {
		return fmt.Errorf("report saved but reset failed: %w", err)
	}
	return nil
}

func deliverReport(cfg *config.Config, sum deliver.Summary) {
	if cfg.Report.Email != "" {
		acct := cfg.AccountByName(cfg.Report.FromAccount)
		if acct == nil {
			log.Printf("report email skipped: from_account %q not found", cfg.Report.FromAccount)
		} else if client, err := gmail.NewClient(cfg.OAuth.ClientID, cfg.OAuth.ClientSecret, acct.RefreshToken); err != nil {
			log.Printf("report email skipped: oauth for %s: %v", acct.Name, err)
		} else if err := deliver.Email(client, acct.Email, cfg.Report.Email, sum); err != nil {
			log.Printf("report email failed: %v", err)
		} else {
			log.Printf("report email sent to %s", cfg.Report.Email)
		}
	}

	if cfg.Report.ArgusWebhookURL != "" && cfg.Report.ArgusWebhookSecret != "" {
		if err := deliver.EchoPush(cfg.Report.ArgusWebhookURL, cfg.Report.ArgusWebhookSecret, sum); err != nil {
			log.Printf("report echo push failed: %v", err)
		} else {
			log.Printf("report pushed to Echo")
		}
	}
}
