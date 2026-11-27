package pgcue

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/postgres"
)

// Session owns a lazy, reusable database connection. Call Close when finished.
// It is intended for sequential operations, not concurrent use.
type Session struct {
	DSN string
	// Timeout limits each operation; 0 means no limit. PostgreSQL's statement_timeout
	// applies independently.
	Timeout time.Duration
	// Warn receives visibility warnings before execution. A failure stops execution.
	// A nil Warn disables visibility warnings.
	Warn func(string) error

	conn              *pgx.Conn
	environment       *databaseEnvironment
	visibilityChecked bool
}

// Connect establishes the session if it is not already connected, within Timeout.
func (s *Session) Connect(ctx context.Context) error {
	if s.conn != nil && !s.conn.IsClosed() {
		return nil
	}
	// A connection closed by a cancelled query may still be cancelling it on the server;
	// closing it waits for that. The old connection's close error does not affect this one.
	_ = s.Close()
	connectCtx, cancel := operationContext(ctx, s.Timeout)
	defer cancel()
	conn, err := postgres.Connect(connectCtx, s.DSN)
	if err != nil {
		return operationError(ctx, connectCtx, s.Timeout, "connection", err)
	}
	s.conn = conn
	return nil
}

// Close releases the session independently of the operation context.
func (s *Session) Close() error {
	s.environment, s.visibilityChecked = nil, false
	if s.conn == nil {
		return nil
	}
	err := postgres.Close(s.conn)
	s.conn = nil
	return err
}

func (s *Session) database() postgres.Executor { return postgres.Executor{Conn: s.conn} }

// withEnvironment connects, applies Timeout, and inspects dependencies before calling fn.
// Timeouts are reported against name.
func (s *Session) withEnvironment(
	ctx context.Context,
	name string,
	dependencies []string,
	fn func(context.Context, databaseEnvironment) error,
) error {
	if err := s.Connect(ctx); err != nil {
		return err
	}
	operationCtx, cancel := operationContext(ctx, s.Timeout)
	defer cancel()
	run := func() error {
		environment, err := s.inspectEnvironment(operationCtx, dependencies)
		if err != nil {
			return err
		}
		return fn(operationCtx, environment)
	}
	return operationError(ctx, operationCtx, s.Timeout, name, run())
}

// Execute consumes a check result while its timeout and resource scope are active.
// The consumer must finish reading rows before returning.
func (s *Session) Execute(
	ctx context.Context,
	check catalogue.Check,
	arguments pgx.NamedArgs,
	consume func(postgres.Result) error,
) error {
	return s.withEnvironment(ctx, check.ID, check.Dependencies(),
		func(ctx context.Context, environment databaseEnvironment) error {
			if err := s.warnVisibility(check, environment.connectionInfo); err != nil {
				return err
			}
			result, err := executeCheck(ctx, s.database(), environment, check, arguments)
			if err != nil {
				return err
			}
			defer result.Close()
			return consume(result)
		})
}

func (s *Session) warnVisibility(check catalogue.Check, info postgres.ConnectionInfo) error {
	if !check.RequiresMonitor || s.Warn == nil || s.visibilityChecked {
		return nil
	}
	if !info.ReadAllStats {
		if err := s.Warn(
			"monitoring privileges are limited; other sessions' query text and statistics may be hidden. Results may be incomplete.",
		); err != nil {
			return err
		}
	}
	s.visibilityChecked = true
	return nil
}

// Target inspects the connected database without executing a check.
func (s *Session) Target(ctx context.Context) (Target, error) {
	var target Target
	err := s.withEnvironment(
		ctx,
		"server details",
		nil,
		func(_ context.Context, environment databaseEnvironment) error {
			target = Target{Address: postgres.Target(s.conn.Config()), ConnectionInfo: environment.connectionInfo}
			return nil
		},
	)
	return target, err
}

// Doctor inspects server support, connection safety and catalogue dependencies.
func (s *Session) Doctor(ctx context.Context, catalog *catalogue.Catalogue) (DoctorReport, error) {
	var report DoctorReport
	err := s.withEnvironment(ctx, "doctor", catalog.Dependencies(),
		func(_ context.Context, environment databaseEnvironment) error {
			report = doctorReport(environment, catalog, postgres.Target(s.conn.Config()))
			return nil
		})
	return report, err
}

// Availability reports which checks match the connected database environment.
func (s *Session) Availability(ctx context.Context, catalog *catalogue.Catalogue) (map[string]bool, error) {
	var availability map[string]bool
	err := s.withEnvironment(ctx, "check availability", catalog.Dependencies(),
		func(_ context.Context, environment databaseEnvironment) (err error) {
			availability, err = inspectAvailability(environment, catalog)
			return err
		})
	return availability, err
}
