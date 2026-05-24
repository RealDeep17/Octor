-- Migration: drop selected_files column from vault.resource table in octor DB
ALTER TABLE vault.resource DROP COLUMN selected_files;
