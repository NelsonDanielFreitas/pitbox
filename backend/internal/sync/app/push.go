package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/domain"
)

type PushInput struct {
	DeviceID uuid.UUID
	Pass     string
	Ops      []domain.Operation
}

// OpResult is the outcome of one operation. A rejection is a result, never an RPC error.
type OpResult struct {
	OpID    uuid.UUID
	Status  domain.Status
	Reason  string
	Version *int32
}

// rejection aborts the transaction of one operation. record says whether the rejected
// operation is kept for the owner to review.
type rejection struct {
	reason string
	record bool
}

func (r *rejection) Error() string { return "sync: operation rejected: " + r.reason }

// errRaceLost means another writer got to the entity (or the same op_id) first. The
// transaction is rolled back and the operation is tried again from the current state.
var errRaceLost = errors.New("sync: lost a race, retry")

// gate is what is known about the device before looking at any operation.
type gate struct {
	device     domain.Device
	reason     string    // non-empty: every operation is rejected with it
	passExpiry time.Time // operations made after this (plus skew) are not covered
}

// Push applies the operations in order, each in its own transaction. It is idempotent on
// op_id: sending the same queue again returns the stored results and changes nothing.
func (s *Service) Push(ctx context.Context, user db.UserID, team db.TeamID, in PushInput) ([]OpResult, error) {
	// Any member may open a Push (a pilot sends the driver's feedback); what each operation
	// may write is decided per entity from the caller's current role (mayWrite).
	if err := s.checkMember(ctx, user.String(), team.String()); err != nil {
		return nil, err
	}
	g, err := s.loadGate(ctx, user, team, in)
	if err != nil {
		return nil, err
	}
	results := make([]OpResult, 0, len(in.Ops))
	for _, op := range in.Ops {
		r, err := s.pushOne(ctx, user, team, g, op)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

func (s *Service) loadGate(ctx context.Context, user db.UserID, team db.TeamID, in PushInput) (gate, error) {
	var g gate
	err := s.db.WithTeamReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
		if !canPush(m.Role) {
			return fmt.Errorf("mechanic role: %w", domain.ErrForbidden)
		}
		dev, err := s.store.Device(ctx, tx, uuid.UUID(team), in.DeviceID)
		if err != nil {
			return err
		}
		// Someone else's device is the same answer as a missing one.
		if dev.UserID != uuid.UUID(user) {
			return fmt.Errorf("device: %w", domain.ErrNotFound)
		}
		g.device = dev
		if dev.Revoked {
			g.reason = domain.ReasonDeviceRevoked
			return nil
		}
		g.reason = domain.ReasonPassInvalid
		claims, openErr := s.verifier.Open(in.Pass)
		if openErr != nil || claims.TeamID != uuid.UUID(team) || claims.DeviceID != dev.ID {
			return nil //nolint:nilerr // an unusable pass is a per-operation rejection, not an RPC error
		}
		pass, err := s.store.Pass(ctx, tx, uuid.UUID(team), claims.PassID)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			return nil
		case err != nil:
			return err
		}
		if pass.Revoked || pass.DeviceID != dev.ID || pass.UserID != uuid.UUID(user) {
			return nil
		}
		g.reason, g.passExpiry = "", pass.ExpiresAt
		return nil
	})
	return g, err
}

func (s *Service) pushOne(ctx context.Context, user db.UserID, team db.TeamID, g gate, op domain.Operation) (OpResult, error) {
	if !signatureOK(g.device.PublicKey, uuid.UUID(team), g.device.ID, op) {
		// Not recorded: nothing proves the device sent it, and it must not burn the op_id.
		s.log.WarnContext(ctx, "sync: bad operation signature")
		return OpResult{OpID: op.OpID, Status: domain.StatusRejected, Reason: domain.ReasonBadSignature}, nil
	}

	reason := g.reason
	now := s.now()
	if reason == "" && (op.ClientTS.After(now.Add(clockSkew)) || op.ClientTS.After(g.passExpiry.Add(clockSkew))) {
		reason = domain.ReasonBadTimestamp
	}

	fields, ferr := domain.ParseFields(op.FieldsJSON)
	applier, known := s.appliers[op.Entity]
	for attempt := 0; attempt < maxOpRetries; attempt++ {
		out, err := s.applyInTx(ctx, user, team, g.device.ID, op, fields, ferr, applier, known, reason)
		var rej *rejection
		switch {
		case err == nil:
			return out, nil
		case errors.As(err, &rej):
			return s.reject(ctx, user, team, g.device.ID, op, rej, ferr == nil)
		case errors.Is(err, errRaceLost):
			continue
		default:
			return OpResult{}, err
		}
	}
	// Still contended: let the device send it again. Everything before is already stored.
	return OpResult{}, fmt.Errorf("operation contended: %w", domain.ErrBusy)
}

func (s *Service) applyInTx(
	ctx context.Context, user db.UserID, team db.TeamID, deviceID uuid.UUID, op domain.Operation,
	fields map[string]json.RawMessage, fieldsErr error, applier syncapply.Applier, known bool, gateReason string,
) (OpResult, error) {
	var out OpResult
	err := s.db.WithTeamTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
		// Idempotency first: a stored op_id answers with what happened, whatever the device's
		// state is now.
		stored, found, err := s.store.Operation(ctx, tx, op.OpID)
		if err != nil {
			return err
		}
		if found {
			if stored.TeamID != uuid.UUID(team) || stored.DeviceID != deviceID || stored.Entity != op.Entity || stored.EntityID != op.EntityID {
				return &rejection{reason: domain.ReasonOpReplayed}
			}
			out = OpResult{OpID: op.OpID, Status: stored.Status, Version: stored.ResultVersion}
			if stored.Reason != nil {
				out.Reason = *stored.Reason
			}
			return nil
		}

		switch {
		case gateReason != "":
			return &rejection{reason: gateReason, record: true}
		case !mayWrite(m.Role, applier):
			return &rejection{reason: domain.ReasonForbidden, record: true}
		case !known:
			return &rejection{reason: domain.ReasonUnknownEntity, record: true}
		case fieldsErr != nil || !validKind(op):
			return &rejection{reason: domain.ReasonInvalidFields, record: true}
		}

		res, err := s.execute(ctx, tx, team, user, m.Role, op, fields, applier)
		if err != nil {
			return err
		}
		rec := domain.StoredOp{
			OpID: op.OpID, TeamID: uuid.UUID(team), UserID: uuid.UUID(user), DeviceID: deviceID,
			Entity: op.Entity, EntityID: op.EntityID, Kind: op.Kind, BaseVersion: op.BaseVersion,
			Fields: op.FieldsJSON, ClientTS: op.ClientTS, Signature: op.Signature,
			Status: res.status, ResultVersion: &res.version,
		}
		if err := s.store.InsertOperation(ctx, tx, rec); err != nil {
			if errors.Is(err, domain.ErrDuplicateOp) {
				return errRaceLost
			}
			return err
		}
		for _, c := range res.conflicts {
			if err := s.store.InsertConflict(ctx, tx, uuid.UUID(team), domain.Conflict{
				OpID: op.OpID, Entity: op.Entity, EntityID: op.EntityID, Field: c.Field,
				ServerValue: c.Server, ClientValue: c.Client,
			}); err != nil {
				return err
			}
		}
		if err := s.store.TouchDevice(ctx, tx, uuid.UUID(team), deviceID); err != nil {
			return err
		}
		if err := audited(ctx, tx, "sync.op."+string(res.status), op.Entity, op.EntityID.String(),
			map[string]any{"op_id": op.OpID, "kind": op.Kind, "version": res.version}, op.OpID.String()); err != nil {
			return err
		}
		v := res.version
		out = OpResult{OpID: op.OpID, Status: res.status, Version: &v}
		return nil
	})
	return out, err
}

func signatureOK(publicKey []byte, team, device uuid.UUID, op domain.Operation) bool {
	pub, err := domain.ParsePublicKey(publicKey)
	return err == nil && domain.VerifyOperation(pub, team, device, op) == nil
}

func validKind(op domain.Operation) bool {
	switch op.Kind {
	case domain.KindCreate, domain.KindAppend:
		return true
	case domain.KindPatch, domain.KindDelete:
		return op.BaseVersion != nil
	default:
		return false
	}
}

type execResult struct {
	status    domain.Status
	version   int32
	conflicts []domain.FieldConflict
}

var syncKinds = map[domain.Kind]syncapply.Kind{
	domain.KindCreate: syncapply.Create, domain.KindPatch: syncapply.Patch,
	domain.KindAppend: syncapply.Append, domain.KindDelete: syncapply.Delete,
}

// execute applies one validated operation to its entity, merging field by field when the
// device's base version is behind.
func (s *Service) execute(
	ctx context.Context, tx db.DBTX, team db.TeamID, user db.UserID, role string,
	op domain.Operation, fields map[string]json.RawMessage, a syncapply.Applier,
) (execResult, error) {
	apply := func(expected int32, f map[string]json.RawMessage) (int32, error) {
		v, err := a.Apply(ctx, tx, syncapply.Op{
			OpID: op.OpID, TeamID: team, UserID: user, Role: role, Kind: syncKinds[op.Kind],
			EntityID: op.EntityID, Fields: f, ExpectedVersion: expected,
		})
		return v, mapApplyErr(err)
	}

	if op.Kind == domain.KindCreate || op.Kind == domain.KindAppend {
		v, err := apply(0, fields)
		return execResult{status: domain.StatusApplied, version: v}, err
	}

	cur, err := a.CurrentVersion(ctx, tx, team, op.EntityID)
	if err != nil {
		return execResult{}, mapApplyErr(err)
	}
	base := *op.BaseVersion
	if base > cur {
		return execResult{}, &rejection{reason: domain.ReasonInvalidFields, record: true}
	}

	res := execResult{status: domain.StatusApplied, version: cur}
	toApply := fields
	if base < cur {
		newer, err := s.store.AppliedSince(ctx, tx, uuid.UUID(team), op.Entity, op.EntityID, base)
		if err != nil {
			return execResult{}, err
		}
		if op.Kind == domain.KindDelete {
			// Someone changed it since: deleting is the mechanic's call, not ours.
			res.status = domain.StatusConflict
			res.conflicts = []domain.FieldConflict{{Field: "_deleted", Client: json.RawMessage("true")}}
			return res, nil
		}
		m := domain.Merge(fields, newer)
		toApply, res.conflicts = m.Apply, m.Conflicts
		res.status = m.Status(newer)
		if res.status == domain.StatusApplied {
			// Newer versions exist but no recorded operation explains them (rows from before
			// sync): the merge was blind, so say so.
			res.status = domain.StatusMerged
		}
	}
	if op.Kind == domain.KindPatch && len(toApply) == 0 {
		return res, nil // everything clashed: nothing to write
	}
	res.version, err = apply(cur, toApply)
	return res, err
}

func mapApplyErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syncapply.ErrStale):
		return errRaceLost
	case errors.Is(err, syncapply.ErrInvalidFields):
		return &rejection{reason: domain.ReasonInvalidFields, record: true}
	case errors.Is(err, syncapply.ErrInvalidRef):
		return &rejection{reason: domain.ReasonInvalidRef, record: true}
	case errors.Is(err, syncapply.ErrCatalogRetired):
		return &rejection{reason: domain.ReasonCatalogRetired, record: true}
	case errors.Is(err, syncapply.ErrNotFound):
		return &rejection{reason: domain.ReasonNotFound, record: true}
	case errors.Is(err, syncapply.ErrForbidden):
		return &rejection{reason: domain.ReasonForbidden, record: true}
	default:
		return err
	}
}

// reject answers a rejected operation and, when asked, records it for review in a new
// transaction (the failed one was rolled back, so nothing it did is left behind).
func (s *Service) reject(ctx context.Context, user db.UserID, team db.TeamID, deviceID uuid.UUID, op domain.Operation, rej *rejection, fieldsOK bool) (OpResult, error) {
	res := OpResult{OpID: op.OpID, Status: domain.StatusRejected, Reason: rej.reason}
	if !rej.record {
		return res, nil
	}
	reason := rej.reason
	rec := domain.StoredOp{
		OpID: op.OpID, TeamID: uuid.UUID(team), UserID: uuid.UUID(user), DeviceID: deviceID,
		Entity: op.Entity, EntityID: op.EntityID, Kind: op.Kind, BaseVersion: op.BaseVersion,
		ClientTS: op.ClientTS, Signature: op.Signature, Status: domain.StatusRejected, Reason: &reason,
	}
	if fieldsOK {
		rec.Fields = op.FieldsJSON
	}
	err := s.db.WithTeamTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		return s.store.InsertOperation(ctx, tx, rec)
	})
	if err != nil && !errors.Is(err, domain.ErrDuplicateOp) {
		return OpResult{}, err
	}
	return res, nil
}
