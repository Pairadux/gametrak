package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/austincgause/gametrak/internal/config"
	"github.com/austincgause/gametrak/internal/logbook"
	"github.com/austincgause/gametrak/internal/notify"
	"github.com/austincgause/gametrak/internal/tracker"
	"github.com/austincgause/gametrak/internal/utility"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync sessions with Logbook now",
	Long: `Upload recorded sessions to a Logbook sheet and mirror the sheet back.

Sessions without a row on the site are uploaded, oldest first, so the first
sync uploads your whole history. Then the site wins: edits and deletions made
there replace the local copy, and sessions added there are added here.

While the daemon runs it syncs by itself: when it starts, after every logged
session, and every logbook.poll_mins minutes (0 turns polling off). Running
this command syncs now, pulls the whole sheet even if it looks unchanged, and
tries again any session the site rejected before.

Setup:
  1. In config.yaml (url and sheet show their defaults):
       logbook:
         enabled: true
         url: ` + config.DefaultLogbookURL + `
         sheet: ` + config.DefaultLogbookSheet + `
  2. Put the Cloudflare Access service token in
     ` + config.DefaultLogbookCredentials + `
     (or the file logbook.credentials_file names):
       client_id: <Client ID>
       client_secret: <Client Secret>
     and make it readable only by you:
       chmod 600 ` + config.DefaultLogbookCredentials + `
  3. Run gametrak sync, then restart the daemon.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !cfg.Logbook.Enabled {
			return errors.New("Logbook sync is off; set logbook.enabled: true in config.yaml (see gametrak sync --help)")
		}
		syncer, err := newSyncer(func(format string, args ...any) { fmt.Printf(format+"\n", args...) })
		if err != nil {
			return err
		}

		result, err := syncer.Sync(context.Background(), true)
		if summary := describeSync(result); summary != "" {
			fmt.Println(capitalize(summary))
		} else if err == nil {
			fmt.Printf("Already in step with %s\n", syncer.Client.Target())
		}
		return err
	},
}

// newSyncer builds a syncer from the logbook config. The sync state and lock
// sit next to the daemon's state file.
func newSyncer(logf func(format string, args ...any)) (*logbook.Syncer, error) {
	creds, err := logbook.LoadCredentials(cfg.Logbook.CredentialsFile)
	if err != nil {
		return nil, err
	}
	stateDir := filepath.Dir(cfg.Settings.StateFile)
	return &logbook.Syncer{
		Client:       logbook.NewClient(cfg.Logbook.URL, cfg.Logbook.Sheet, creds),
		SessionsFile: cfg.Settings.SessionsFile,
		StateFile:    filepath.Join(stateDir, "logbook.json"),
		LockFile:     filepath.Join(stateDir, "logbook.lock"),
		Location:     time.Local,
		Logf:         logf,
	}, nil
}

// startSync syncs with Logbook in the background when it is enabled: right
// away, after every logged session, and every poll_mins minutes. The returned
// function stops it, giving a sync in progress a moment to finish.
func startSync(track *tracker.Tracker) (stop func()) {
	if !cfg.Logbook.Enabled {
		return func() {}
	}
	syncer, err := newSyncer(logf)
	if err != nil {
		logf("Warning: Logbook sync is off: %v", err)
		if cfg.Settings.Notifications {
			notify.SyncFailed(err.Error())
		}
		return func() {}
	}

	trigger := make(chan struct{}, 1)
	poke := func() {
		select {
		case trigger <- struct{}{}:
		default: // a sync is already due
		}
	}
	track.OnLogged(poke)
	poke()

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		syncer.Run(ctx, trigger, time.Duration(cfg.Logbook.PollMins)*time.Minute, reportSync())
	}()
	logf("Syncing with Logbook: %s", syncer.Client.Target())

	return func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
		}
	}
}

// reportSync logs each sync that did something and each new failure, once
// rather than every poll while offline. Failures that retrying won't fix,
// like a rejected token, also raise a notification.
func reportSync() func(logbook.Result, error) {
	lastErr := ""
	return func(result logbook.Result, err error) {
		if summary := describeSync(result); summary != "" {
			logf("Logbook: %s", summary)
		}
		switch {
		case err != nil && err.Error() != lastErr:
			lastErr = err.Error()
			logf("Warning: Logbook sync failed: %v", err)
			if cfg.Settings.Notifications && !logbook.Temporary(err) {
				notify.SyncFailed(err.Error())
			}
		case err == nil && lastErr != "":
			lastErr = ""
			logf("Logbook sync is working again")
		}
	}
}

// describeSync summarizes a sync, or returns "" if it changed nothing.
func describeSync(result logbook.Result) string {
	var parts []string
	if result.Uploaded > 0 {
		parts = append(parts, "uploaded "+utility.Plural(result.Uploaded, "session"))
	}
	c := result.Changes
	var pulled []string
	for _, change := range []struct {
		count int
		label string
	}{{c.Added, "added"}, {c.Updated, "updated"}, {c.Deleted, "deleted"}, {c.Linked, "linked"}} {
		if change.count > 0 {
			pulled = append(pulled, fmt.Sprintf("%d %s", change.count, change.label))
		}
	}
	if len(pulled) > 0 {
		parts = append(parts, "from the site: "+strings.Join(pulled, ", "))
	}
	if result.Rejected > 0 {
		parts = append(parts, utility.Plural(result.Rejected, "session")+" rejected")
	}
	return strings.Join(parts, "; ")
}

func capitalize(s string) string {
	return strings.ToUpper(s[:1]) + s[1:]
}

func init() {
	rootCmd.AddCommand(syncCmd)
}
