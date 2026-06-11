CREATE TABLE tpdb_studio
(
    name       TEXT PRIMARY KEY,
    poster_url TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tpdb_performer
(
    name       TEXT PRIMARY KEY,
    poster_url TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Trigger for updated_at in tpdb_studio
CREATE FUNCTION update_tpdb_studio_updated_at_column()
    RETURNS TRIGGER AS
$$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_updated_at_tpdb_studio
    BEFORE UPDATE
    ON tpdb_studio
    FOR EACH ROW
EXECUTE FUNCTION update_tpdb_studio_updated_at_column();

-- Trigger for updated_at in tpdb_performer
CREATE FUNCTION update_tpdb_performer_updated_at_column()
    RETURNS TRIGGER AS
$$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_updated_at_tpdb_performer
    BEFORE UPDATE
    ON tpdb_performer
    FOR EACH ROW
EXECUTE FUNCTION update_tpdb_performer_updated_at_column();
