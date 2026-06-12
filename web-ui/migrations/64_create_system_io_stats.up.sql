CREATE TABLE public.system_io_stats (
    id SERIAL PRIMARY KEY,
    timestamp TIMESTAMPTZ NOT NULL,
    disk_read_bytes BIGINT NOT NULL,
    disk_write_bytes BIGINT NOT NULL,
    net_rx_bytes BIGINT NOT NULL,
    net_tx_bytes BIGINT NOT NULL
);
CREATE INDEX idx_system_io_stats_timestamp ON public.system_io_stats(timestamp);
