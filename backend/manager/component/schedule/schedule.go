// Package schedule parses reminder cron expressions and computes the next
// fire time. It isolates the cron library dependency (github.com/robfig/cron/v3)
// from both the scheduler (which fires reminders) and the API layer (which
// reschedules recurring reminders on completion/miss), so neither has to reach
// into cron internals.
package schedule

import (
	"time"

	"github.com/pkg/errors"
	cron "github.com/robfig/cron/v3"
)

// parser is the 5-field cron parser (min hour dom month dow) shared across
// calls. robfig/cron's default parser also accepts descriptors (@daily etc.)
// and 6-7 field expressions; we restrict to the standard 5-field form so the
// schedule is portable and unambiguous.
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// Validate parses cronExpr in the given IANA timezone and returns an error if
// the expression is invalid or the timezone cannot be loaded. tz may be empty
// (treated as UTC).
func Validate(cronExpr, tz string) error {
	if _, err := parser.Parse(cronExpr); err != nil {
		return errors.Wrapf(err, "invalid cron expression %q", cronExpr)
	}
	if _, err := loadLocation(tz); err != nil {
		return err
	}
	return nil
}

// NextFire returns the next fire time after from for the given cron expression,
// interpreted in tz. tz may be empty (UTC). Returns the zero time and an error
// if the expression or timezone is invalid.
func NextFire(cronExpr, tz string, from time.Time) (time.Time, error) {
	loc, err := loadLocation(tz)
	if err != nil {
		return time.Time{}, err
	}
	sched, err := parser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "invalid cron expression %q", cronExpr)
	}
	return sched.Next(from.In(loc)), nil
}

// loadLocation loads an IANA timezone name, treating "" as UTC. Returns an
// error if the name is not a known timezone.
func loadLocation(tz string) (*time.Location, error) {
	if tz == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid timezone %q", tz)
	}
	return loc, nil
}
