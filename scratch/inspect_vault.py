import psycopg2

try:
    conn = psycopg2.connect(host="127.0.0.1", port=5432, user="postgres", password="postgres", database="vault")
    cur = conn.cursor()
    cur.execute("SELECT resource_id, status, total_size, stored_size, claim_expires_at FROM resource;")
    rows = cur.fetchall()
    print(f"=== VAULT RESOURCE TABLE ({len(rows)} rows) ===")
    for r in rows:
        print(f"ID: {r[0]}, Status: {r[1]}, TotalSize: {r[2]}, StoredSize: {r[3]}, ClaimExpires: {r[4]}")
    cur.close()
    conn.close()
except Exception as e:
    print(f"Error: {e}")
