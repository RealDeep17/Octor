#!/usr/bin/env python3
import os
import sys
import re
import shutil
import argparse
import psycopg2

def load_env(filepath):
    if not os.path.exists(filepath):
        return
    with open(filepath) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith('#'):
                continue
            parts = line.split('=', 1)
            if len(parts) == 2:
                key = parts[0].strip()
                val = parts[1].strip().strip('"\'')
                if not os.environ.get(key):
                    os.environ[key] = val

def main():
    parser = argparse.ArgumentParser(description="Octor Storage Orphan Cleanup Utility")
    parser.add_argument("--dry-run", type=str, default="true", help="Set to 'false' to actually delete files")
    args = parser.parse_args()
    
    dry_run = args.dry_run.lower() != "false"

    # Load custom env files
    load_env("custom.env")
    load_env("../custom.env")
    load_env("/home/ubuntu/octor/custom.env")

    # Get DB credentials
    db_user = os.environ.get("PG_USER", "octor")
    db_pass = os.environ.get("PG_PASSWORD", "")
    db_host = os.environ.get("PG_HOST", "localhost")
    db_port = os.environ.get("PG_PORT", "5432")

    # Connect to vault database
    try:
        conn = psycopg2.connect(
            dbname="vault",
            user=db_user,
            password=db_pass,
            host=db_host,
            port=db_port
        )
    except Exception as e:
        print(f"Error: Failed to connect to database: {e}")
        sys.exit(1)

    cur = conn.cursor()

    # Query active resource IDs
    try:
        cur.execute("SELECT resource_id FROM resource;")
        active_resources = set(row[0] for row in cur.fetchall())
    except Exception as e:
        print(f"Error: Failed to query active resources: {e}")
        sys.exit(1)

    # Query active file hashes
    try:
        cur.execute("SELECT hash FROM file;")
        active_hashes = set(row[0] for row in cur.fetchall())
    except Exception as e:
        print(f"Error: Failed to query active file hashes: {e}")
        sys.exit(1)

    cur.close()
    conn.close()

    print("=" * 60)
    print("Starting Octor Storage Garbage Collection")
    print(f"Mode: DRY-RUN = {dry_run}")
    print(f"Active Resources in DB: {len(active_resources)}")
    print(f"Active File Hashes in DB: {len(active_hashes)}")
    print("=" * 60)

    # Get storage path
    storage_path = os.environ.get("VAULT_STORAGE_PATH") or os.environ.get("S3_GATEWAY_STORAGE_DIR") or "/home/ubuntu/octor/infra-data/drive-mount-vfs"
    
    if not os.path.exists(storage_path):
        print(f"Error: Storage path does not exist: {storage_path}")
        sys.exit(1)

    orphaned_metadata_files = []
    orphaned_torrent_files = []
    orphaned_vault_dirs = []

    # 1. Check recovery metadata (metadata/resources/<id>.json)
    meta_dir = os.path.join(storage_path, "recovery", "metadata", "resources")
    if os.path.exists(meta_dir):
        for filename in os.listdir(meta_dir):
            if filename.endswith(".json"):
                infohash = filename[:-5]
                if infohash not in active_resources:
                    orphaned_metadata_files.append(os.path.join(meta_dir, filename))

    # 2. Check recovery torrents (recovery/torrents/<name> [<id>].torrent)
    torrent_dir = os.path.join(storage_path, "recovery", "torrents")
    if os.path.exists(torrent_dir):
        # Regex to extract infohash from the brackets in filename
        hash_re = re.compile(r"\[([a-fA-F0-9]{40})\]\.torrent$")
        for filename in os.listdir(torrent_dir):
            if filename.endswith(".torrent") and not filename.startswith("."):
                match = hash_re.search(filename)
                if match:
                    infohash = match.group(1).lower()
                    if infohash not in active_resources:
                        orphaned_torrent_files.append(os.path.join(torrent_dir, filename))
                else:
                    # If format doesn't match, keep it safe
                    pass

    # 3. Check vault files (vault/<hash>/<hash>)
    vault_dir = os.path.join(storage_path, "vault")
    if os.path.exists(vault_dir):
        for entry in os.listdir(vault_dir):
            entry_path = os.path.join(vault_dir, entry)
            if os.path.isdir(entry_path) and len(entry) == 40:
                file_hash = entry.lower()
                if file_hash not in active_hashes:
                    orphaned_vault_dirs.append(entry_path)

    # Print summary
    print(f"Found {len(orphaned_metadata_files)} orphaned metadata files")
    print(f"Found {len(orphaned_torrent_files)} orphaned torrent files")
    print(f"Found {len(orphaned_vault_dirs)} orphaned vault directories")
    print("-" * 60)

    # Perform cleanup or reporting
    total_freed_bytes = 0

    if orphaned_metadata_files:
        print("\nOrphaned Metadata Files:")
        for path in orphaned_metadata_files:
            print(f"  - {os.path.basename(path)}")
            if not dry_run:
                try:
                    os.remove(path)
                except Exception as e:
                    print(f"    [Error deleting]: {e}")

    if orphaned_torrent_files:
        print("\nOrphaned Torrent Files:")
        for path in orphaned_torrent_files:
            print(f"  - {os.path.basename(path)}")
            if not dry_run:
                try:
                    os.remove(path)
                except Exception as e:
                    print(f"    [Error deleting]: {e}")

    if orphaned_vault_dirs:
        print("\nOrphaned Vault Directories:")
        for path in orphaned_vault_dirs:
            # Try to calculate size
            dir_size = 0
            try:
                for root, dirs, files in os.walk(path):
                    for f in files:
                        dir_size += os.path.getsize(os.path.join(root, f))
            except:
                pass
            total_freed_bytes += dir_size
            
            print(f"  - {os.path.basename(path)} ({dir_size / (1024*1024):.2f} MB)")
            if not dry_run:
                try:
                    shutil.rmtree(path)
                except Exception as e:
                    print(f"    [Error deleting]: {e}")

    print("\n" + "=" * 60)
    if dry_run:
        print(f"DRY-RUN COMPLETE. Would delete {len(orphaned_metadata_files) + len(orphaned_torrent_files)} files and {len(orphaned_vault_dirs)} directories.")
        print(f"Estimated space freed: {total_freed_bytes / (1024*1024*1024):.2f} GB")
        print("To apply changes, run with: python3 scripts/clean_orphans.py --dry-run=false")
    else:
        print(f"CLEANUP COMPLETE. Deleted {len(orphaned_metadata_files) + len(orphaned_torrent_files)} files and {len(orphaned_vault_dirs)} directories.")
        print(f"Freed: {total_freed_bytes / (1024*1024*1024):.2f} GB")
    print("=" * 60)

if __name__ == "__main__":
    main()
