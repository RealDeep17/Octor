-- Migration: drop selected_files column from resource table
ALTER TABLE resource DROP COLUMN selected_files;
