package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/pgcue"
)

func (app *App) doctor(ctx context.Context, _, options []string) error {
	dsn, timeout, err := parseConnectionOptions(options)
	if err != nil {
		return inputError(err)
	}
	return app.withSession(dsn, timeout, func(session *pgcue.Session) error {
		report, err := session.Doctor(ctx, app.catalogue)
		if err != nil {
			return err
		}
		return app.writeDoctor(report)
	})
}

func (app *App) writeDoctor(report pgcue.DoctorReport) error {
	style := app.Style
	var text strings.Builder
	fmt.Fprintln(&text, style.Heading("PostgreSQL readiness"))
	ok, warning, failure := style.Green("✓"), style.Yellow("!"), style.Red("✗")
	writeDoctorLine(&text, ok, "Connected to "+report.Target)
	writeDoctorLine(&text, ok, "Server role: "+report.Connection.ServerRole)
	if err := catalogue.CheckServerVersion(report.Connection.ServerMajorVersion); err != nil {
		writeDoctorLine(&text, failure, err.Error())
	} else {
		writeDoctorLine(&text, ok, fmt.Sprintf("PostgreSQL %d is supported", report.Connection.ServerMajorVersion))
	}
	writeDoctorLine(&text, ok,
		fmt.Sprintf(
			"Connected as %s to database %s",
			report.Connection.User,
			report.Connection.Database,
		),
	)
	if report.Connection.ReadOnly {
		writeDoctorLine(&text, ok, "Session is read-only")
	} else {
		writeDoctorLine(&text, failure, "Session is not read-only")
	}
	if report.Connection.PGMonitor {
		writeDoctorLine(&text, ok, "Role can read PostgreSQL monitoring views")
	} else {
		writeDoctorLine(&text, warning,
			"Role is not a member of pg_monitor; some rows or columns may be hidden",
		)
	}
	fmt.Fprintf(&text, "\n%s\n", style.Bold("Optional extensions"))
	for _, extension := range report.Extensions {
		checks := strings.Join(extension.Checks, ", ")
		switch {
		case !extension.Installed():
			writeDoctorLine(&text, warning,
				fmt.Sprintf("%s is not installed; unavailable: %s", extension.Name, checks),
			)
		case catalogue.VersionAtLeast(extension.Name, extension.Version, extension.Minimum):
			writeDoctorLine(&text, ok,
				fmt.Sprintf("%s %s is supported", extension.Name, extension.Version),
			)
		default:
			writeDoctorLine(&text, warning,
				fmt.Sprintf(
					"%s %s is below the supported minimum %s; unavailable: %s",
					extension.Name,
					extension.Version,
					extension.Minimum,
					checks,
				),
			)
		}
	}
	if _, err := io.WriteString(app.Out, text.String()); err != nil {
		return err
	}
	return report.Err()
}

func writeDoctorLine(text *strings.Builder, marker, message string) {
	fmt.Fprintf(text, "  %s %s\n", marker, message)
}
