package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"

	uuid "github.com/satori/go.uuid"
)

type User struct {
	tableName struct{}  `pg:"user"`
	UserID    uuid.UUID `pg:"user_id,pk"`
	Email     string
	Password  string
	CreatedAt time.Time
	UpdatedAt time.Time
	Tier      string
}

// GetOrCreateUser finds or creates a user by email.
func GetOrCreateUser(ctx context.Context, db *pg.DB, email string) (*User, bool, error) {
	user := &User{}

	// Find by email
	err := db.Model(user).
		Context(ctx).
		Where("email = ?", email).
		Limit(1).
		Select()
	if err == nil {
		return user, false, nil
	}
	if !errors.Is(err, pg.ErrNoRows) {
		return nil, false, err // DB error
	}

	// Create new user
	user.Email = email
	_, err = db.Model(user).
		Context(ctx).
		Insert()
	if err != nil {
		return nil, false, err
	}
	return user, true, nil
}

func DeleteUser(ctx context.Context, db *pg.DB, userID uuid.UUID) error {
	_, err := db.Model((*User)(nil)).
		Context(ctx).
		Where("user_id = ?", userID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to delete user")
	}
	return nil
}

func UpdateUserTier(ctx context.Context, db *pg.DB, u *User) error {
	_, err := db.Model(u).
		Context(ctx).
		WherePK().
		Column("tier").
		Update()
	return err
}
