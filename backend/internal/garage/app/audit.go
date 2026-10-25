package app

import (
	"context"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/audit"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

func auditEvent(ctx context.Context, tx db.DBTX, action, objectType, objectID string, before, after any, requestID string) error {
	return audit.RecordTeam(ctx, tx, audit.TeamEvent{
		Action: action, ObjectType: objectType, ObjectID: objectID,
		Before: before, After: after, RequestID: requestID,
	})
}
