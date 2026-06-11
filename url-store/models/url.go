package models

import (
	"time"
)

type URL struct {
	tableName  struct{}  `pg:"public.url,alias:u"`
	URL        string    `pg:"url,pk,notnull"`
	CreatedAt  time.Time `pg:",default:now()"`
	AccessedAt time.Time `pg:",default:now()"`
}
