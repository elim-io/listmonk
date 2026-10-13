package main

import (
	"github.com/knadh/listmonk/internal/cloudrun"
	"github.com/knadh/listmonk/internal/tmptokens"
)

// cronJobs returns the jobs that Cloud Scheduler runs through POST /cron/<name>
// when cloud_run.cron is on, in place of the timers inside the process. Each
// job still follows its on/off setting; Scheduler's schedule replaces the
// interval setting.
func (a *App) cronJobs() map[string]cloudrun.CronJob {
	return map[string]cloudrun.CronJob{
		// See initCron().
		"slow-query-cache": func() (bool, error) {
			if !ko.Bool("app.cache_slow_queries") {
				return false, nil
			}
			return true, a.core.RefreshMatViews(true)
		},
		"db-vacuum": func() (bool, error) {
			if !ko.Bool("maintenance.db.vacuum") {
				return false, nil
			}
			return true, RunDBVacuum(a.db, a.log)
		},

		// See checkUpdates(). The update status is kept in the memory of the
		// instance that runs the check.
		"update-check": func() (bool, error) {
			if !ko.Bool("app.check_updates") {
				return false, nil
			}
			return true, a.checkUpdate(versionString)
		},

		// Tokens are kept in the memory of each instance: this cleans the
		// instance that gets the call.
		"token-cleanup": func() (bool, error) {
			tmptokens.Clean()
			return true, nil
		},
	}
}
