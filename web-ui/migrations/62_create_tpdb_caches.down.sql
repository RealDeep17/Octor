DROP TRIGGER IF EXISTS trg_updated_at_tpdb_performer ON tpdb_performer;
DROP FUNCTION IF EXISTS update_tpdb_performer_updated_at_column();
DROP TABLE IF EXISTS tpdb_performer;

DROP TRIGGER IF EXISTS trg_updated_at_tpdb_studio ON tpdb_studio;
DROP FUNCTION IF EXISTS update_tpdb_studio_updated_at_column();
DROP TABLE IF EXISTS tpdb_studio;
