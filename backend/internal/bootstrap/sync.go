package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/store/sqlcgen"
)

// syncCatalog gives the sync module the global catalogues through the public API of the garage
// (scales, powertrains) and of the setup module (sheet templates). They are reference data with
// no team context, so the pool is used directly.
type syncCatalog struct {
	garage    *garage.CatalogReader
	templates *setup.CatalogueReader
}

type catalogPool interface {
	store.Querier
	sqlcgen.DBTX
}

func newSyncCatalog(q catalogPool) syncCatalog {
	return syncCatalog{garage: garage.NewCatalogReader(q), templates: setup.NewCatalogueReader(q)}
}

// Load returns the catalogue version (a hash of the garage catalogue and of the templates) and,
// only when the device's version differs, the snapshot.
func (c syncCatalog) Load(ctx context.Context, have string) (string, json.RawMessage, error) {
	cat, err := c.garage.Load(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("load catalogue: %w", err)
	}
	tplVersion, err := c.templates.Version(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("load template catalogue version: %w", err)
	}
	sum := sha256.Sum256([]byte(cat.Version + ":" + tplVersion))
	version := hex.EncodeToString(sum[:])
	if have == version {
		return version, nil, nil
	}

	type scale struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		Active    bool   `json:"active"`
		SortOrder int16  `json:"sort_order"`
	}
	type powertrain struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		Family    string `json:"family"`
		Active    bool   `json:"active"`
		SortOrder int16  `json:"sort_order"`
	}
	templates, err := c.templates.Snapshot(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("load template catalogue: %w", err)
	}
	out := struct {
		Scales         []scale                   `json:"scales"`
		Powertrains    []powertrain              `json:"powertrains"`
		SheetTemplates []setup.CatalogueTemplate `json:"sheet_templates"`
	}{
		Scales: make([]scale, 0, len(cat.Scales)), Powertrains: make([]powertrain, 0, len(cat.Powertrains)),
		SheetTemplates: templates,
	}
	for _, s := range cat.Scales {
		out.Scales = append(out.Scales, scale{s.Code, s.Name, s.Active, s.SortOrder})
	}
	for _, p := range cat.Powertrains {
		out.Powertrains = append(out.Powertrains, powertrain{p.Code, p.Name, p.Family, p.Active, p.SortOrder})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", nil, fmt.Errorf("encode catalogue: %w", err)
	}
	return version, raw, nil
}
