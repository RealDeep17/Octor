-- Migration: add selected_files text array to vault.resource table in octor DB
ALTER TABLE vault.resource ADD COLUMN selected_files TEXT[];
