package models

type Claims struct {
	tableName     struct{} `pg:",discard_unknown_columns"`
	Email         string   `pg:"email"`
	PatreonUserID string   `pg:"patreon_user_id"`
	TierID        uint32   `pg:"tier_id"`
	TierName      string   `pg:"tier_name"`
	DownloadRate  *uint64  `pg:"download_rate"`
	VaultPoints   *uint64  `pg:"vault_points"`
	EmbedNoAds    bool     `pg:"embed_noads"`
	SiteNoAds     bool     `pg:"site_noads"`
}
