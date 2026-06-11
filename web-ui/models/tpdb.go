package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
)

type TpdbStudio struct {
	tableName struct{} `pg:"tpdb_studio"`

	Name      string    `pg:"name,pk"`
	PosterURL string    `pg:"poster_url"`
	UpdatedAt time.Time `pg:"updated_at,default:now()"`
}

type TpdbPerformer struct {
	tableName struct{} `pg:"tpdb_performer"`

	Name      string    `pg:"name,pk"`
	PosterURL string    `pg:"poster_url"`
	UpdatedAt time.Time `pg:"updated_at,default:now()"`
}

func GetTpdbStudio(ctx context.Context, db *pg.DB, name string) (*TpdbStudio, error) {
	s := &TpdbStudio{}
	err := db.Model(s).Context(ctx).Where("name = ?", name).Select()
	if err == pg.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func GetTpdbPerformer(ctx context.Context, db *pg.DB, name string) (*TpdbPerformer, error) {
	p := &TpdbPerformer{}
	err := db.Model(p).Context(ctx).Where("name = ?", name).Select()
	if err == pg.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func UpsertTpdbStudio(ctx context.Context, db *pg.DB, s *TpdbStudio) error {
	_, err := db.Model(s).Context(ctx).OnConflict("(name) DO UPDATE").Set("poster_url = EXCLUDED.poster_url, updated_at = now()").Insert()
	return err
}

func UpsertTpdbPerformer(ctx context.Context, db *pg.DB, p *TpdbPerformer) error {
	_, err := db.Model(p).Context(ctx).OnConflict("(name) DO UPDATE").Set("poster_url = EXCLUDED.poster_url, updated_at = now()").Insert()
	return err
}
