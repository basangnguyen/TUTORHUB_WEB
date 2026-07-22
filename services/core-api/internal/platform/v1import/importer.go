package v1import

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Mode string

const (
	ModeDryRun Mode = "dry-run"
	ModeApply  Mode = "apply"
)

var (
	ErrDatabaseURLRequired  = errors.New("V1 fixture import database URL is required")
	ErrEnvironmentBlocked   = errors.New("V1 fixture import is blocked in this environment")
	ErrDirectURLRequired    = errors.New("V1 fixture import requires a direct database URL")
	ErrInjectedInterruption = errors.New("V1 fixture import interrupted for resume test")
	ErrNaturalKeyConflict   = errors.New("V1 fixture import natural key conflicts with an unmapped target")
	ErrMappedTargetMissing  = errors.New("V1 fixture import mapping target is missing")
)

type Options struct {
	// StopAfter is test-only. Production CLI never exposes it.
	StopAfter int
}

type Counts struct {
	Source    int `json:"source"`
	Mapped    int `json:"mapped"`
	Imported  int `json:"imported"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

type Issue struct {
	EntityType EntityType `json:"entity_type"`
	ExternalID string     `json:"external_id"`
	ReasonCode string     `json:"reason_code"`
}

type Report struct {
	Mode              Mode                  `json:"mode"`
	Status            string                `json:"status"`
	SourceSystem      string                `json:"source_system"`
	FixtureKey        string                `json:"fixture_key"`
	FixtureSHA256     string                `json:"fixture_sha256"`
	RunID             string                `json:"run_id,omitempty"`
	CheckpointOrdinal int                   `json:"checkpoint_ordinal"`
	TotalRecords      int                   `json:"total_records"`
	FailureAttempts   int                   `json:"failure_attempts"`
	Entities          map[EntityType]Counts `json:"entities"`
	Total             Counts                `json:"total"`
	Issues            []Issue               `json:"issues"`
}

type recordOutcome string

const (
	outcomeImported  recordOutcome = "imported"
	outcomeUpdated   recordOutcome = "updated"
	outcomeUnchanged recordOutcome = "unchanged"
	outcomeSkipped   recordOutcome = "skipped"
)

type runState struct {
	ID                uuid.UUID
	Status            string
	CheckpointOrdinal int
	TotalRecords      int
	FailureAttempts   int
	LastErrorCode     string
}

func ValidateEnvironment(environment string) error {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "development", "test", "staging":
		return nil
	default:
		return ErrEnvironmentBlocked
	}
}

func Execute(
	ctx context.Context,
	databaseURL string,
	environment string,
	parsed ParsedFixture,
	mode Mode,
	options Options,
) (Report, error) {
	if err := ValidateEnvironment(environment); err != nil {
		return Report{}, err
	}
	if strings.TrimSpace(databaseURL) == "" {
		return Report{}, ErrDatabaseURLRequired
	}
	if mode != ModeDryRun && mode != ModeApply {
		return Report{}, fmt.Errorf("unsupported V1 fixture import mode %q", mode)
	}

	plan, err := buildPlan(parsed.Fixture)
	if err != nil {
		return Report{}, fmt.Errorf("build V1 fixture import plan: %w", err)
	}

	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return Report{}, fmt.Errorf("parse V1 fixture import database configuration: %w", err)
	}
	if strings.Contains(strings.ToLower(config.Host), "-pooler") {
		return Report{}, ErrDirectURLRequired
	}
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	config.RuntimeParams["application_name"] = "tutorhub-v1-fixture-import"
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return Report{}, fmt.Errorf("connect V1 fixture import database: %w", err)
	}
	defer connection.Close(context.Background())

	if mode == ModeDryRun {
		return executeDryRun(ctx, connection, parsed, plan)
	}
	return executeApply(ctx, connection, parsed, plan, options)
}

func executeDryRun(ctx context.Context, connection *pgx.Conn, parsed ParsedFixture, plan []plannedRecord) (Report, error) {
	transaction, err := connection.Begin(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("begin V1 fixture dry-run: %w", err)
	}
	defer transaction.Rollback(context.Background())

	results := make([]processedRecord, 0, len(plan))
	for _, record := range plan {
		result := processedRecord{Record: record}
		if record.SkipReason != "" {
			result.Outcome = outcomeSkipped
			result.ReasonCode = record.SkipReason
		} else {
			outcome, targetID, processErr := processRecord(ctx, transaction, parsed.Fixture.SourceSystem, record)
			if processErr != nil {
				result.ErrorCode = safeErrorCode(processErr)
				results = append(results, result)
				report := buildDryRunReport(parsed, plan, results)
				report.Status = "failed"
				return report, fmt.Errorf("dry-run %s record %d: %w", record.EntityType, record.Ordinal, processErr)
			}
			result.Outcome = outcome
			result.TargetID = targetID
		}
		results = append(results, result)
	}

	report := buildDryRunReport(parsed, plan, results)
	report.Status = "planned"
	return report, nil
}

func executeApply(
	ctx context.Context,
	connection *pgx.Conn,
	parsed ParsedFixture,
	plan []plannedRecord,
	options Options,
) (Report, error) {
	run, err := acquireRun(ctx, connection, parsed, len(plan))
	if err != nil {
		return Report{}, err
	}
	if len(plan) == 0 {
		if err := completeEmptyRun(ctx, connection, run.ID); err != nil {
			return Report{}, err
		}
		return loadApplyReport(ctx, connection, parsed, plan, run.ID)
	}

	processedThisInvocation := 0
	for ordinal := run.CheckpointOrdinal; ordinal < len(plan); ordinal++ {
		if options.StopAfter > 0 && processedThisInvocation >= options.StopAfter {
			if markErr := markRunFailed(ctx, connection, run.ID, "injected_interruption"); markErr != nil {
				return Report{}, errors.Join(ErrInjectedInterruption, markErr)
			}
			report, reportErr := loadApplyReport(ctx, connection, parsed, plan, run.ID)
			return report, errors.Join(ErrInjectedInterruption, reportErr)
		}

		if err := applyRecord(ctx, connection, parsed.Fixture.SourceSystem, run.ID, plan[ordinal]); err != nil {
			errorCode := safeErrorCode(err)
			markErr := markRunFailed(ctx, connection, run.ID, errorCode)
			report, reportErr := loadApplyReport(ctx, connection, parsed, plan, run.ID)
			return report, errors.Join(fmt.Errorf("apply %s record %d: %w", plan[ordinal].EntityType, ordinal, err), markErr, reportErr)
		}
		processedThisInvocation++
	}

	return loadApplyReport(ctx, connection, parsed, plan, run.ID)
}

type processedRecord struct {
	Record     plannedRecord
	Outcome    recordOutcome
	ReasonCode string
	ErrorCode  string
	TargetID   uuid.UUID
}

func buildDryRunReport(parsed ParsedFixture, plan []plannedRecord, results []processedRecord) Report {
	report := newReport(ModeDryRun, parsed, plan)
	report.CheckpointOrdinal = len(results)
	for _, result := range results {
		counts := report.Entities[result.Record.EntityType]
		switch {
		case result.ErrorCode != "":
			counts.Failed++
			report.Issues = append(report.Issues, Issue{EntityType: result.Record.EntityType, ExternalID: result.Record.ExternalID, ReasonCode: result.ErrorCode})
		case result.Outcome == outcomeSkipped:
			counts.Skipped++
			report.Issues = append(report.Issues, Issue{EntityType: result.Record.EntityType, ExternalID: result.Record.ExternalID, ReasonCode: result.ReasonCode})
		default:
			incrementOutcome(&counts, result.Outcome)
		}
		report.Entities[result.Record.EntityType] = counts
	}
	finalizeReportTotals(&report)
	return report
}

func newReport(mode Mode, parsed ParsedFixture, plan []plannedRecord) Report {
	entities := map[EntityType]Counts{
		EntityUser:       {},
		EntityTenant:     {},
		EntityMembership: {},
		EntityClass:      {},
	}
	for _, record := range plan {
		counts := entities[record.EntityType]
		counts.Source++
		entities[record.EntityType] = counts
	}
	return Report{
		Mode:          mode,
		SourceSystem:  parsed.Fixture.SourceSystem,
		FixtureKey:    parsed.Fixture.FixtureKey,
		FixtureSHA256: hex.EncodeToString(parsed.SHA256[:]),
		TotalRecords:  len(plan),
		Entities:      entities,
		Issues:        make([]Issue, 0),
	}
}

func incrementOutcome(counts *Counts, outcome recordOutcome) {
	switch outcome {
	case outcomeImported:
		counts.Imported++
		counts.Mapped++
	case outcomeUpdated:
		counts.Updated++
		counts.Mapped++
	case outcomeUnchanged:
		counts.Unchanged++
		counts.Mapped++
	case outcomeSkipped:
		counts.Skipped++
	}
}

func finalizeReportTotals(report *Report) {
	var total Counts
	for _, entityType := range []EntityType{EntityUser, EntityTenant, EntityMembership, EntityClass} {
		counts := report.Entities[entityType]
		total.Source += counts.Source
		total.Mapped += counts.Mapped
		total.Imported += counts.Imported
		total.Updated += counts.Updated
		total.Unchanged += counts.Unchanged
		total.Skipped += counts.Skipped
		total.Failed += counts.Failed
	}
	report.Total = total
}

func safeErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrNaturalKeyConflict):
		return "natural_key_conflict"
	case errors.Is(err, ErrMappedTargetMissing):
		return "mapped_target_missing"
	case errors.Is(err, ErrInjectedInterruption):
		return "injected_interruption"
	default:
		return "record_apply_failed"
	}
}

func sourceHashBytes(hash [sha256.Size]byte) []byte {
	return hash[:]
}
