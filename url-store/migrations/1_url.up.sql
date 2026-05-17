CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE public.url
(
    url text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    accessed_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT url_pkey PRIMARY KEY (url)
);

CREATE INDEX url_idx ON url (digest(url, 'sha1'));