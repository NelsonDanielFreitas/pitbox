package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	// MaxOwnedTeams is the maximum number of teams a user may own.
	MaxOwnedTeams   = 10
	DefaultCategory = "1/8 TT nitro"
	maxName         = 80
	maxCategory     = 40
	maxClub         = 80
)

// Team is a racing team (the unit of data isolation).
type Team struct {
	ID              uuid.UUID
	Name            string
	Category        string
	Club            *string
	CountryCode     string
	CreatedBy       uuid.UUID
	CreatedAt       time.Time
	Version         int32
	PendingTransfer *OwnershipTransfer
}

// NewTeam validates and normalises the fields of a new team. The id, author and
// timestamps come from the database.
func NewTeam(name, category string, club *string, countryCode string) (Team, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxName {
		return Team{}, fmt.Errorf("%w: team name", ErrInvalid)
	}
	category = strings.TrimSpace(category)
	if category == "" {
		category = DefaultCategory
	}
	if utf8.RuneCountInString(category) > maxCategory {
		return Team{}, fmt.Errorf("%w: team category", ErrInvalid)
	}
	normalizedClub, err := normalizeClub(club)
	if err != nil {
		return Team{}, err
	}
	if !ValidCountryCode(countryCode) {
		return Team{}, fmt.Errorf("%w: country code", ErrInvalid)
	}
	return Team{Name: name, Category: category, Club: normalizedClub, CountryCode: countryCode}, nil
}

// TeamUpdate is the part of a team an UpdateTeam request changes. A nil pointer
// means "not in the update mask". ClubSet distinguishes "clear the club" (true
// with Club nil) from "do not touch the club" (false).
type TeamUpdate struct {
	Name        *string
	Category    *string
	Club        *string
	ClubSet     bool
	CountryCode *string
}

// Empty reports whether the mask selected nothing.
func (u TeamUpdate) Empty() bool {
	return u.Name == nil && u.Category == nil && u.CountryCode == nil && !u.ClubSet
}

// ApplyTeamUpdate validates the selected fields and returns the updated team.
// Only the club may be cleared; name, category and country are required.
func ApplyTeamUpdate(t Team, u TeamUpdate) (Team, error) {
	if u.Empty() {
		return Team{}, fmt.Errorf("%w: empty update mask", ErrInvalid)
	}
	if u.Name != nil {
		name := strings.TrimSpace(*u.Name)
		if name == "" || utf8.RuneCountInString(name) > maxName {
			return Team{}, fmt.Errorf("%w: team name", ErrInvalid)
		}
		t.Name = name
	}
	if u.Category != nil {
		category := strings.TrimSpace(*u.Category)
		if category == "" || utf8.RuneCountInString(category) > maxCategory {
			return Team{}, fmt.Errorf("%w: team category", ErrInvalid)
		}
		t.Category = category
	}
	if u.CountryCode != nil {
		if !ValidCountryCode(*u.CountryCode) {
			return Team{}, fmt.Errorf("%w: country code", ErrInvalid)
		}
		t.CountryCode = *u.CountryCode
	}
	if u.ClubSet {
		club, err := normalizeClub(u.Club)
		if err != nil {
			return Team{}, err
		}
		t.Club = club
	}
	return t, nil
}

func normalizeClub(club *string) (*string, error) {
	if club == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*club)
	if value == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(value) > maxClub {
		return nil, fmt.Errorf("%w: team club", ErrInvalid)
	}
	return &value, nil
}
