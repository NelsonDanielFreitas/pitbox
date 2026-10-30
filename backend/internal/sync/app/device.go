package app

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/domain"
)

type RegisterDeviceInput struct {
	Name      string
	Platform  string
	PublicKey []byte
	Proof     []byte
	RequestID uuid.UUID
}

// RegisterDevice stores a device key after checking that the caller holds its private key.
// Registering the same key again returns the same device (idempotent).
func (s *Service) RegisterDevice(ctx context.Context, user db.UserID, team db.TeamID, in RegisterDeviceInput) (uuid.UUID, error) {
	pub, err := domain.ParsePublicKey(in.PublicKey)
	if err != nil {
		return uuid.Nil, fmt.Errorf("public key: %w", domain.ErrInvalid)
	}
	if err := domain.VerifyRegistration(pub, uuid.UUID(team), uuid.UUID(user), in.PublicKey, in.Proof); err != nil {
		return uuid.Nil, fmt.Errorf("proof of possession: %w", domain.ErrInvalid)
	}
	var id uuid.UUID
	err = s.withMemberTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		existing, found, err := s.store.DeviceByPublicKey(ctx, tx, uuid.UUID(team), uuid.UUID(user), in.PublicKey)
		if err != nil {
			return err
		}
		if found {
			id = existing
			return nil
		}
		n, err := s.store.ActiveDevices(ctx, tx, uuid.UUID(team), uuid.UUID(user))
		if err != nil {
			return err
		}
		if n >= maxDevicesPerUser {
			return fmt.Errorf("device limit: %w", domain.ErrLimit)
		}
		id, err = s.store.InsertDevice(ctx, tx, uuid.UUID(team), uuid.UUID(user), in.Name, in.Platform, in.PublicKey)
		if err != nil {
			return err
		}
		return audited(ctx, tx, "sync.device.register", "identity.device_key", id.String(),
			map[string]string{"platform": in.Platform}, in.RequestID.String())
	})
	return id, err
}

// IssueOfflinePass gives a registered, non-revoked device of the caller a pass that lasts
// at most 72 hours. The role in the pass is the CURRENT one.
func (s *Service) IssueOfflinePass(ctx context.Context, user db.UserID, team db.TeamID, deviceID uuid.UUID) (string, time.Time, error) {
	var token string
	var expires time.Time
	err := s.withMemberTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, role string) error {
		dev, err := s.store.Device(ctx, tx, uuid.UUID(team), deviceID)
		if err != nil {
			return err
		}
		if dev.UserID != uuid.UUID(user) || dev.Revoked {
			return fmt.Errorf("device: %w", domain.ErrNotFound)
		}
		// The database sets the expiry from its own clock: one source of time, and the
		// 72 h CHECK on the table can never be tripped by clock drift.
		pass, err := s.store.InsertPass(ctx, tx, uuid.UUID(team), uuid.UUID(user), deviceID, role, domain.PassTTL)
		if err != nil {
			return err
		}
		expires = pass.ExpiresAt
		token, err = s.signer.Sign(domain.PassClaims{
			PassID: pass.ID, TeamID: uuid.UUID(team), DeviceID: deviceID, Role: role, Expires: pass.ExpiresAt.Unix(),
		})
		if err != nil {
			return fmt.Errorf("sign pass: %w", err)
		}
		return audited(ctx, tx, "sync.pass.issue", "identity.offline_pass", pass.ID.String(), map[string]string{"role": role}, "")
	})
	return token, expires, err
}

// RevokeDevice lets the owner revoke any device and a user revoke their own. Revoking also
// revokes its passes; later operations from it are rejected and kept for review.
func (s *Service) RevokeDevice(ctx context.Context, user db.UserID, team db.TeamID, deviceID, requestID uuid.UUID) error {
	return s.withMemberTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, role string) error {
		dev, err := s.store.Device(ctx, tx, uuid.UUID(team), deviceID)
		if err != nil {
			return err
		}
		if role != "owner" && dev.UserID != uuid.UUID(user) {
			return fmt.Errorf("revoke device: %w", domain.ErrNotFound)
		}
		if _, err := s.store.RevokeDevice(ctx, tx, uuid.UUID(team), deviceID); err != nil {
			return err
		}
		return audited(ctx, tx, "sync.device.revoke", "identity.device_key", deviceID.String(), nil, requestID.String())
	})
}
