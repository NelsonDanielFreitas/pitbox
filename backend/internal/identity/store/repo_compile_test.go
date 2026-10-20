package store_test

import (
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/store"
)

var _ app.Store = (*store.Repo)(nil)
