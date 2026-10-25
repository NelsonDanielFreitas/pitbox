// Package garage is the garage module. Other modules use only this file.
package garage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

// CarDirectory answers whether a car exists in a team. The caller must already have
// authorized the team and passes its own transaction: the query runs under that
// transaction's RLS context. It deliberately does not open a transaction.
type CarDirectory struct{ repo *store.Repo }

func NewCarDirectory() *CarDirectory { return &CarDirectory{repo: store.NewRepo()} }

func (d *CarDirectory) Exists(ctx context.Context, tx db.DBTX, teamID db.TeamID, carID uuid.UUID) (bool, error) {
	return d.repo.CarExists(ctx, tx, uuid.UUID(teamID), carID)
}

// Scale is one global scale entry.
type Scale struct {
	Code      string
	Name      string
	Active    bool
	SortOrder int16
}

// Powertrain is one global powertrain entry.
type Powertrain struct {
	Code      string
	Name      string
	Family    string
	Active    bool
	SortOrder int16
}

// Catalog is the deterministic snapshot used by the sync layer.
type Catalog struct {
	Version     string
	Scales      []Scale
	Powertrains []Powertrain
}

// CatalogReader reads the global catalogues and their content hash. There is no team
// context: the catalogues are reference data.
type CatalogReader struct{ db store.Querier }

func NewCatalogReader(db store.Querier) *CatalogReader { return &CatalogReader{db: db} }

func (r *CatalogReader) Load(ctx context.Context) (Catalog, error) {
	repo := store.NewRepo()
	scales, err := repo.Scales(ctx, r.db)
	if err != nil {
		return Catalog{}, err
	}
	powertrains, err := repo.Powertrains(ctx, r.db)
	if err != nil {
		return Catalog{}, err
	}
	c := Catalog{
		Scales:      make([]Scale, 0, len(scales)),
		Powertrains: make([]Powertrain, 0, len(powertrains)),
	}
	h := sha256.New()
	writeField(h, "scale")
	writeField(h, strconv.Itoa(len(scales)))
	for _, s := range scales {
		c.Scales = append(c.Scales, Scale{Code: s.Code, Name: s.Name, Active: s.Active, SortOrder: s.SortOrder})
		writeField(h, s.Code)
		writeField(h, s.Name)
		writeField(h, boolString(s.Active))
		writeField(h, strconv.Itoa(int(s.SortOrder)))
	}
	writeField(h, "powertrain")
	writeField(h, strconv.Itoa(len(powertrains)))
	for _, p := range powertrains {
		c.Powertrains = append(c.Powertrains, Powertrain{Code: p.Code, Name: p.Name, Family: p.Family, Active: p.Active, SortOrder: p.SortOrder})
		writeField(h, p.Code)
		writeField(h, p.Name)
		writeField(h, p.Family)
		writeField(h, boolString(p.Active))
		writeField(h, strconv.Itoa(int(p.SortOrder)))
	}
	c.Version = hex.EncodeToString(h.Sum(nil))
	return c, nil
}

func writeField(h interface{ Write([]byte) (int, error) }, value string) {
	_, _ = h.Write([]byte(strconv.Itoa(len(value))))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(value))
}

func boolString(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
