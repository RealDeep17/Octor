-- Migration: add selected_files text array to resource table
ALTER TABLE resource ADD COLUMN selected_files TEXT[];
