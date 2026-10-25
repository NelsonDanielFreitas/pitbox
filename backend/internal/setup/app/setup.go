package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/cursor"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

const (
	setupCreateMethod   = "/pitbox.setup.v1.SetupService/CreateSetup"
	versionCreateMethod = "/pitbox.setup.v1.SetupService/CreateVersion"
)

type CreateSetupInput struct {
	RequestID     uuid.UUID
	CarID         uuid.UUID
	Name          string
	Notes         *string
	SourceSetupID *uuid.UUID
	SourceVersion *int32
}

type CreateVersionInput struct {
	RequestID     uuid.UUID
	SetupID       uuid.UUID
	BaseVersion   int32
	Data          map[string]any
	ChangeSummary *string
}

type idCursor struct {
	ID uuid.UUID `json:"id"`
}

type versionCursor struct {
	Version int32 `json:"version"`
}

func (s *Service) CreateSetup(ctx context.Context, user db.UserID, team db.TeamID, in CreateSetupInput) (domain.Setup, error) {
	name, err := domain.NormalizeName(in.Name)
	if err != nil {
		return domain.Setup{}, err
	}
	notes, err := domain.NormalizeNotes(in.Notes)
	if err != nil {
		return domain.Setup{}, err
	}
	if in.SourceVersion != nil && in.SourceSetupID == nil {
		return domain.Setup{}, fmt.Errorf("source version without setup: %w", domain.ErrInvalid)
	}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.Setup{}, fmt.Errorf("hash create setup: %w", err)
	}

	var out domain.Setup
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, setupCreateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Setup](response)
			return err
		}
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, setupCreateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Setup](response)
			return err
		}
		if ok, err := s.garage.CarExists(ctx, tx, team, in.CarID); err != nil {
			return fmt.Errorf("check car: %w", err)
		} else if !ok {
			return fmt.Errorf("check car: %w", domain.ErrNotFound)
		}

		data := map[string]any{}
		var basedOnSetup *uuid.UUID
		var basedOnVersion *int32
		if in.SourceSetupID != nil {
			sourceVersion := int32(0)
			if in.SourceVersion != nil {
				sourceVersion = *in.SourceVersion
			}
			source, err := s.store.GetVersion(ctx, tx, uuid.UUID(team), *in.SourceSetupID, sourceVersion)
			if err != nil {
				return err
			}
			if source.Ref.SchemaVersion != domain.SchemaVersion {
				return fmt.Errorf("source schema: %w", domain.ErrInvalid)
			}
			if _, err := domain.MarshalV1(source.Data); err != nil {
				return err
			}
			data = source.Data
			basedOnSetup = in.SourceSetupID
			basedOnVersion = &source.Ref.Version
		}

		created, err := s.store.CreateSetup(ctx, tx, uuid.UUID(team), uuid.UUID(user), in.CarID, name, notes)
		if err != nil {
			return err
		}
		raw, err := domain.MarshalV1(data)
		if err != nil {
			return err
		}
		createdID, err := uuid.Parse(created.ID)
		if err != nil {
			return err
		}
		if _, err := s.store.CreateVersion(ctx, tx, uuid.UUID(team), uuid.UUID(user), createdID, 1, raw, basedOnSetup, basedOnVersion, nil); err != nil {
			return err
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, created); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "setup.create", "setup", created.ID, nil, setupAudit(created), in.RequestID.String()); err != nil {
			return err
		}
		out = created
		return nil
	})
	return out, err
}

func (s *Service) CreateVersion(ctx context.Context, user db.UserID, team db.TeamID, in CreateVersionInput) (domain.VersionRef, error) {
	if in.BaseVersion < 1 {
		return domain.VersionRef{}, fmt.Errorf("base version: %w", domain.ErrInvalid)
	}
	data, err := domain.MarshalV1(in.Data)
	if err != nil {
		return domain.VersionRef{}, err
	}
	summary, err := domain.NormalizeChangeSummary(in.ChangeSummary)
	if err != nil {
		return domain.VersionRef{}, err
	}
	hash, err := hashInput(uuid.UUID(team), struct {
		SetupID     uuid.UUID      `json:"setup_id"`
		BaseVersion int32          `json:"base_version"`
		Data        map[string]any `json:"data"`
		Summary     *string        `json:"change_summary"`
	}{in.SetupID, in.BaseVersion, in.Data, summary})
	if err != nil {
		return domain.VersionRef{}, fmt.Errorf("hash setup version: %w", err)
	}

	var out domain.VersionRef
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, versionCreateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.VersionRef](response)
			return err
		}
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, versionCreateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.VersionRef](response)
			return err
		}
		current, err := s.store.LockSetup(ctx, tx, uuid.UUID(team), in.SetupID)
		if err != nil {
			return err
		}
		if current.CurrentVersion != in.BaseVersion {
			return domain.ErrConflict
		}
		base, err := s.store.GetVersion(ctx, tx, uuid.UUID(team), in.SetupID, in.BaseVersion)
		if err != nil {
			return err
		}
		if err := s.validateChangedReferences(ctx, tx, team, base.Data, in.Data); err != nil {
			return err
		}
		next := in.BaseVersion + 1
		createdRef, err := s.store.CreateVersion(ctx, tx, uuid.UUID(team), uuid.UUID(user), in.SetupID, next, data, &in.SetupID, &in.BaseVersion, summary)
		if err != nil {
			return err
		}
		if _, err := s.store.UpdateCurrentVersion(ctx, tx, uuid.UUID(team), in.SetupID, next, in.BaseVersion); err != nil {
			return err
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, createdRef); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "setup.version.create", "setup_version", in.SetupID.String(), nil, versionAudit(createdRef), in.RequestID.String()); err != nil {
			return err
		}
		out = createdRef
		return nil
	})
	return out, err
}

func (s *Service) GetVersion(ctx context.Context, user db.UserID, team db.TeamID, setupID uuid.UUID, version *int32) (domain.Version, error) {
	v := int32(0)
	if version != nil {
		v = *version
	}
	var out domain.Version
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		var err error
		out, err = s.store.GetVersion(ctx, tx, uuid.UUID(team), setupID, v)
		return err
	})
	return out, err
}

func (s *Service) ListSetups(ctx context.Context, user db.UserID, team db.TeamID, carID *uuid.UUID, size int32, token string) ([]domain.Setup, string, error) {
	var c idCursor
	if err := cursor.Decode(token, &c); err != nil {
		return nil, "", err
	}
	limit := pageSize(size)
	var out []domain.Setup
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		var err error
		out, err = s.store.ListSetups(ctx, tx, uuid.UUID(team), c.ID, carID, limit+1)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > int(limit) {
		out = out[:limit]
		next = cursor.Encode(idCursor{ID: uuid.MustParse(out[len(out)-1].ID)})
	}
	return out, next, nil
}

func (s *Service) ListVersions(ctx context.Context, user db.UserID, team db.TeamID, setupID uuid.UUID, size int32, token string) ([]domain.VersionRef, string, error) {
	var c versionCursor
	if err := cursor.Decode(token, &c); err != nil {
		return nil, "", err
	}
	before := c.Version
	if before == 0 {
		before = math.MaxInt32
	}
	limit := pageSize(size)
	var out []domain.VersionRef
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		var err error
		out, err = s.store.ListVersions(ctx, tx, uuid.UUID(team), setupID, before, limit+1)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > int(limit) {
		out = out[:limit]
		next = cursor.Encode(versionCursor{Version: out[len(out)-1].Version})
	}
	return out, next, nil
}

func (s *Service) CompareVersions(ctx context.Context, user db.UserID, team db.TeamID, setupID uuid.UUID, from, to int32) ([]domain.Difference, error) {
	var out []domain.Difference
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		a, err := s.store.GetVersion(ctx, tx, uuid.UUID(team), setupID, from)
		if err != nil {
			return err
		}
		b, err := s.store.GetVersion(ctx, tx, uuid.UUID(team), setupID, to)
		if err != nil {
			return err
		}
		out = domain.Compare(a.Data, b.Data)
		return nil
	})
	return out, err
}

func (s *Service) CreateShareLink(ctx context.Context, user db.UserID, team db.TeamID, in CreateShareLinkInput) (domain.ShareLink, string, error) {
	if !domain.ValidShareExpiry(in.Expiry) {
		return domain.ShareLink{}, "", fmt.Errorf("expiry: %w", domain.ErrInvalid)
	}
	if in.PinnedVersion != nil && *in.PinnedVersion < 1 {
		return domain.ShareLink{}, "", fmt.Errorf("pinned version: %w", domain.ErrInvalid)
	}
	var out domain.ShareLink
	var token string
	err := s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		if _, err := s.store.LockSetup(ctx, tx, uuid.UUID(team), in.SetupID); err != nil {
			return err
		}
		if in.PinnedVersion != nil {
			if _, err := s.store.GetVersion(ctx, tx, uuid.UUID(team), in.SetupID, *in.PinnedVersion); err != nil {
				return err
			}
		}
		count, err := s.store.CountActiveShareLinks(ctx, tx, uuid.UUID(team), in.SetupID)
		if err != nil {
			return err
		}
		if count >= 20 {
			return domain.ErrShareLinkFull
		}
		t, h, err := newShareToken()
		if err != nil {
			return err
		}
		token = t
		created, err := s.store.CreateShareLink(ctx, tx, uuid.UUID(team), in.SetupID, uuid.UUID(user), in.PinnedVersion, h, string(in.Expiry))
		if err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "share_link.create", "share_link", created.ID, nil, shareLinkAudit(created), in.RequestID.String()); err != nil {
			return err
		}
		out = created
		return nil
	})
	return out, token, err
}

func (s *Service) ListShareLinks(ctx context.Context, user db.UserID, team db.TeamID, setupID uuid.UUID) ([]domain.ShareLink, error) {
	var out []domain.ShareLink
	err := s.withMechanicReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		if _, err := s.store.GetSetup(ctx, tx, uuid.UUID(team), setupID); err != nil {
			return err
		}
		var err error
		out, err = s.store.ListShareLinks(ctx, tx, uuid.UUID(team), setupID)
		return err
	})
	return out, err
}

func (s *Service) RevokeShareLink(ctx context.Context, user db.UserID, team db.TeamID, setupID, linkID uuid.UUID, requestID uuid.UUID) error {
	return s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		if err := s.store.RevokeShareLink(ctx, tx, uuid.UUID(team), setupID, linkID); err != nil {
			return err
		}
		return s.audit(ctx, tx, "share_link.revoke", "share_link", linkID.String(), nil, map[string]any{
			"share_link_id": linkID.String(), "setup_id": setupID.String(),
		}, requestID.String())
	})
}

func newShareToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate share token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}

func setupAudit(s domain.Setup) map[string]any {
	return map[string]any{
		"car_id": s.CarID, "name": s.Name, "current_version": s.CurrentVersion,
	}
}

func versionAudit(v domain.VersionRef) map[string]any {
	return map[string]any{
		"version": v.Version, "schema_version": v.SchemaVersion,
		"based_on_setup_id": v.BasedOnSetupID, "based_on_version": v.BasedOnVersion,
		"change_summary": v.ChangeSummary,
	}
}

func shareLinkAudit(l domain.ShareLink) map[string]any {
	return map[string]any{
		"share_link_id": l.ID, "setup_id": l.SetupID,
		"pinned_version": l.PinnedVersion, "expires_at": l.ExpiresAt,
	}
}
