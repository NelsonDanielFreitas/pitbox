package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/store"
)

// syncCatalog gives the sync module the global catalogues through the garage's public
// API. They are reference data with no team context, so the pool is used directly.
type syncCatalog struct{ reader *garage.CatalogReader }

func newSyncCatalog(q store.Querier) syncCatalog {
	return syncCatalog{reader: garage.NewCatalogReader(q)}
}

func (c syncCatalog) Load(ctx context.Context) (string, json.RawMessage, error) {
	cat, err := c.reader.Load(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("load catalogue: %w", err)
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
	out := struct {
		Scales      []scale      `json:"scales"`
		Powertrains []powertrain `json:"powertrains"`
	}{Scales: make([]scale, 0, len(cat.Scales)), Powertrains: make([]powertrain, 0, len(cat.Powertrains))}
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
	return cat.Version, raw, nil
}
