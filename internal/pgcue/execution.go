// Package pgcue coordinates catalogue execution and database operations.
package pgcue

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/postgres"
)

type databaseEnvironment struct {
	connectionInfo postgres.ConnectionInfo
	extensions     map[string]postgres.Extension
}

func (environment databaseEnvironment) versions() map[string]string {
	versions := map[string]string{
		catalogue.PostgreSQLDependency: strconv.Itoa(environment.connectionInfo.ServerMajorVersion),
	}
	for name, extension := range environment.extensions {
		versions[name] = extension.Version
	}
	return versions
}

func (environment databaseEnvironment) schemas() map[string]string {
	schemas := make(map[string]string, len(environment.extensions))
	for name, extension := range environment.extensions {
		schemas[name] = extension.Schema
	}
	return schemas
}

func (s *Session) inspectEnvironment(ctx context.Context, dependencies []string) (databaseEnvironment, error) {
	if s.environment == nil {
		connectionInfo, err := s.database().ConnectionInfo(ctx)
		if err != nil {
			return databaseEnvironment{}, err
		}
		s.environment = &databaseEnvironment{
			connectionInfo: connectionInfo,
			extensions:     map[string]postgres.Extension{},
		}
	}
	for _, dependency := range dependencies {
		if dependency == catalogue.PostgreSQLDependency {
			continue
		}
		if _, checked := s.environment.extensions[dependency]; checked {
			continue
		}
		// Missing extensions are recorded too, avoiding repeated lookups.
		extension, err := s.database().LookupExtension(ctx, dependency)
		if err != nil {
			return databaseEnvironment{}, err
		}
		s.environment.extensions[dependency] = extension
	}
	return *s.environment, nil
}

// UnavailableError reports a check this server cannot run, such as one that needs a
// missing extension or a newer PostgreSQL.
type UnavailableError struct{ Err error }

func (e *UnavailableError) Error() string { return e.Err.Error() }

// InvestigationFailed reports the failed checks of an investigation that completed or stopped.
func InvestigationFailed(failed int, stopped bool) error {
	ended, noun := "completed", "checks"
	if stopped {
		ended = "stopped"
	}
	if failed == 1 {
		noun = "check"
	}
	return fmt.Errorf("investigation %s with %d failed %s", ended, failed, noun)
}

func executeCheck(
	ctx context.Context,
	database postgres.Executor,
	environment databaseEnvironment,
	check catalogue.Check,
	arguments pgx.NamedArgs,
) (postgres.Result, error) {
	if err := catalogue.CheckServerVersion(environment.connectionInfo.ServerMajorVersion); err != nil {
		return postgres.Result{}, err
	}
	for _, dependency := range check.Dependencies() {
		extension := environment.extensions[dependency]
		if !extension.Installed() {
			continue
		}
		if err := catalogue.CheckDependencyVersion(dependency, extension.Version); err != nil {
			return postgres.Result{}, &UnavailableError{fmt.Errorf("check %q: %w", check.ID, err)}
		}
	}
	variant, err := check.VariantForEnvironment(environment.versions())
	if err != nil {
		return postgres.Result{}, &UnavailableError{err}
	}
	sql := catalogue.ResolveExtensionSchemas(variant.Statement, environment.schemas())
	return database.Query(ctx, check.ID, sql, arguments)
}

func operationContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithCancel(ctx)
}

func operationError(ctx, operationCtx context.Context, timeout time.Duration, name string, err error) error {
	if err != nil && ctx.Err() == nil && errors.Is(operationCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s timed out after %s: %w", name, timeout, err)
	}
	return err
}
