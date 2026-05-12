CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE abuse (
    abuse_id uuid DEFAULT uuid_generate_v4() NOT NULL,
	notice_id   TEXT,
	work        TEXT,
	filename    TEXT,
	infohash    TEXT,
	description TEXT,
	email       TEXT,
	subject     TEXT,
	cause       SMALLINT,
	source      SMALLINT,
	started_at  timestamp with time zone,
    created_at  timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE abuse OWNER TO abusestore;

ALTER TABLE ONLY abuse
    ADD CONSTRAINT abuse_id PRIMARY KEY (abuse_id);