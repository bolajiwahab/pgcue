package pgcue

import (
	"errors"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/postgres"
)

// DoctorExtension describes a dependency and the checks it enables.
type DoctorExtension struct {
	postgres.Extension
	Minimum catalogue.Version
	Checks  []string
}

// DoctorReport contains database readiness observations.
type DoctorReport struct {
	Target     string
	Connection postgres.ConnectionInfo
	Extensions []DoctorExtension
}

// Target describes a connected database.
type Target struct {
	Address string
	postgres.ConnectionInfo
}

func doctorReport(environment databaseEnvironment, catalog *catalogue.Catalogue, target string) DoctorReport {
	report := DoctorReport{Target: target, Connection: environment.connectionInfo}
	for _, dependency := range catalog.Dependencies() {
		if dependency == catalogue.PostgreSQLDependency {
			continue
		}
		minimum, _ := catalogue.MinimumDependencyVersion(dependency)
		report.Extensions = append(report.Extensions, DoctorExtension{
			Extension: environment.extensions[dependency],
			Minimum:   minimum,
			Checks:    catalog.CheckIDsUsing(dependency),
		})
	}
	return report
}

func inspectAvailability(environment databaseEnvironment, catalog *catalogue.Catalogue) (map[string]bool, error) {
	if err := catalogue.CheckServerVersion(environment.connectionInfo.ServerMajorVersion); err != nil {
		return nil, err
	}
	versions := environment.versions()
	checks := catalog.Checks()
	availability := make(map[string]bool, len(checks))
	for _, check := range checks {
		_, err := check.VariantForEnvironment(versions)
		availability[check.ID] = err == nil
	}
	return availability, nil
}

// Err returns a readiness failure after callers have had a chance to present the report.
func (report DoctorReport) Err() error {
	if err := catalogue.CheckServerVersion(report.Connection.ServerMajorVersion); err != nil {
		return err
	}
	if !report.Connection.ReadOnly {
		return errors.New("PostgreSQL connection is not read-only")
	}
	return nil
}
