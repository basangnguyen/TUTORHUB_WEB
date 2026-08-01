// Package ownertime serializes PostgreSQL schedule writers that can make an
// internal user busy. Keeping the lock authority outside a feature module lets
// Study Meeting and ClassSession writers participate in the same boundary.
package ownertime

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	advisoryLockNamespace = "study-meeting-conflict"
	advisoryLockSQL       = `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`
)

var errInvalidLockScope = errors.New("invalid owner-time lock scope")

// AcquireLocks takes transaction-scoped advisory locks for the requested
// tenant users. UUID sorting gives every multi-user writer the same lock order,
// while the key format deliberately preserves the original Study Meeting key
// so mixed-version deployments continue to serialize one another.
func AcquireLocks(
	ctx context.Context,
	tx pgx.Tx,
	tenantID uuid.UUID,
	userIDs []uuid.UUID,
) error {
	if ctx == nil || tx == nil {
		return errInvalidLockScope
	}
	keys, err := normalizedLockKeys(tenantID, userIDs)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if _, err := tx.Exec(ctx, advisoryLockSQL, key); err != nil {
			return fmt.Errorf("acquire owner-time advisory lock: %w", err)
		}
	}
	return nil
}

func normalizedLockKeys(
	tenantID uuid.UUID,
	userIDs []uuid.UUID,
) ([]string, error) {
	if tenantID == uuid.Nil || len(userIDs) == 0 {
		return nil, errInvalidLockScope
	}
	unique := make(map[uuid.UUID]struct{}, len(userIDs))
	users := make([]uuid.UUID, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID == uuid.Nil {
			return nil, errInvalidLockScope
		}
		if _, exists := unique[userID]; exists {
			continue
		}
		unique[userID] = struct{}{}
		users = append(users, userID)
	}
	sort.Slice(users, func(left int, right int) bool {
		return users[left].String() < users[right].String()
	})
	keys := make([]string, 0, len(users))
	for _, userID := range users {
		keys = append(keys, ownerLockKey(tenantID, userID))
	}
	return keys, nil
}

func ownerLockKey(tenantID uuid.UUID, userID uuid.UUID) string {
	return advisoryLockNamespace + ":" + tenantID.String() + ":" + userID.String()
}
