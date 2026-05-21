#!/bin/bash
# ==============================================================================
# OCTOR 10-MODE AUTOMATED BENCHMARK ORCHESTRATOR (FIXED V2)
# ==============================================================================

set -uo pipefail

RESOURCE_ID="792b3577fed6dd95dbb03f5f0972e821230b834f"
MAGNET="magnet:?xt=urn:btih:792b3577fed6dd95dbb03f5f0972e821230b834f&dn=Project.Hail.Mary.2026.2160p.WEB-DL.DDP5.1.Atmos.H.265-RDNYB.mkv&xl=25055439307&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://opentracker.io:6969/announce&tr=udp://open.stealth.si:80/announce&tr=udp://tracker.opentrackr.org:1337/&tr=udp://tracker.torrent.eu.org:451/announce&tr=udp://bandito.byterunner.io:6969/announce&tr=udp://tracker.qu.ax:6969/announce&tr=http://tracker.renfei.net:8080/announce&tr=udp://open.free-tracker.ga:6969/announce&tr=http://tracker.ipv6tracker.org/announce&tr=udp://tracker2.dler.org:80/announce&tr=udp://exodus.desync.com:6969/announce&tr=udp://open.tracker.cl:1337/announce&tr=udp://open.demonii.com:1337"

LOG_FILE="/srv/octor/benchmark_results.log"
export BENCHMARK_MODE=true
TEST_DURATION=${TEST_DURATION:-300}  # default 600s (10 min) per test, overridable
TELEMETRY_INTERVAL=10   # poll telemetry every 10 seconds (renamed from POLL_INTERVAL to avoid
                        # shadowing the rclone mode config var used by switch_mode.sh)

MODES=(
    "pp-perf-ssd"
    "pp-norm-ssd"
    "pp-perf-ram"
    "pp-norm-ram"
    "sp-perf-ssd"
    "sp-norm-ssd"
    "sp-eco-ssd"
    "sp-perf-ram"
    "sp-norm-ram"
    "sp-eco-ram"
    
)

# Initialize log file
echo "# OCTOR PERFORMANCE BENCHMARK REPORT" > "$LOG_FILE"
echo "Generated: $(date)" >> "$LOG_FILE"
echo "" >> "$LOG_FILE"
echo "| Mode | Speed Avg (MB/s) | Speed Peak (MB/s) | CPU Avg (%) | CPU Peak (%) | RAM Avg (MB) | RAM Peak (MB) | SSD I/O Avg (MB/s) | SSD I/O Peak (MB/s) | Total Transferred (MB) |" >> "$LOG_FILE"
echo "| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |" >> "$LOG_FILE"

get_cpu_use() {
    # Reads /proc/stat twice with 1s interval to get overall system CPU usage %
    read -r cpu a b c d e f g _ < /proc/stat
    local prevtotal=$((a+b+c+d+e+f+g))
    local previdle=$d
    sleep 1
    read -r cpu a b c d e f g _ < /proc/stat
    local total=$((a+b+c+d+e+f+g))
    local idle=$d
    local diff=$((total - prevtotal))
    if [ $diff -eq 0 ]; then
        echo "0.00"
    else
        echo "scale=2; 100 * ($diff - ($idle - $previdle)) / $diff" | bc
    fi
}

get_ram_use() {
    # Total system RAM used in MB
    free -m | awk '/Mem:/ {print $3}'
}

# Detect base block device for /srv/octor
DEV_PATH=$(df /srv/octor | tail -n 1 | awk '{print $1}')
DISK_NAME=$(lsblk -no pkname "$DEV_PATH" | tr -d '[:space:]')
if [ -z "$DISK_NAME" ]; then
    DISK_NAME=$(basename "$DEV_PATH")
fi
STAT_FILE="/sys/class/block/${DISK_NAME}/stat"
echo "Detected SSD/HDD block device: $DISK_NAME (using $STAT_FILE for I/O tracking)"

for MODE in "${MODES[@]}"; do
    echo "=========================================================================="
    echo "🚀 STARTING BENCHMARK FOR MODE: $MODE"
    echo "=========================================================================="

    # 1. Ensure infra containers are up for DB/storage cleanup
    echo "Ensuring infra containers are running for cleanup..."
    for ctr in octor-postgres octor-redis octor-nats; do
        state=$(docker inspect -f '{{.State.Status}}' "$ctr" 2>/dev/null || echo "missing")
        if [ "$state" != "running" ]; then
            echo "  Restarting infra container: $ctr (was: $state)"
            docker start "$ctr" > /dev/null 2>&1 || true
        fi
    done
    # Wait for postgres to be ready before DB cleanup
    for i in {1..15}; do
        docker exec octor-postgres pg_isready -U webtor -q 2>/dev/null && break
        sleep 1
    done

    # 2. Database Cleanup
    echo "Clearing database records for resource $RESOURCE_ID..."
    docker exec -i octor-postgres psql -U webtor -d vault -c "DELETE FROM file WHERE hash IN (SELECT file_hash FROM resource_file WHERE resource_id = '$RESOURCE_ID');" || true
    docker exec -i octor-postgres psql -U webtor -d vault -c "DELETE FROM resource WHERE resource_id = '$RESOURCE_ID';" || true

    # 3. Remote storage cleanup (direct rclone bypasses VFS overhead; no app services needed)
    echo "Wiping remote vault storage (ALPHA_UNION:vault)..."
    rclone --config /home/ubuntu/.config/rclone/rclone.conf purge ALPHA_UNION:vault || true
    rclone --config /home/ubuntu/.config/rclone/rclone.conf mkdir ALPHA_UNION:vault || true

    # 4. Apply performance mode
    # switch_mode.sh handles: stopping all services, unmounting FUSE, wiping badger cache,
    # re-configuring env, re-mounting storage, and starting all app services (incl. web-ui).
    echo "Applying mode $MODE via switch_mode.sh..."
    /srv/octor/switch_mode.sh "$MODE" || {
        echo "❌ Error: Failed to switch to mode $MODE"
        continue
    }

    # 5. Register magnet link to REST API
    echo "Registering test torrent..."
    curl -s -X POST -H "Content-Type: text/plain" -d "$MAGNET" http://localhost:8080/resource/ || {
        echo "⚠️ Warning: Failed to register torrent."
    }

    # 6. Wait for DHT metadata to load
    echo "Waiting for torrent metadata to resolve..."
    METADATA_TIMEOUT=180
    METADATA_ELAPSED=0
    METADATA_OK=false
    while [ $METADATA_ELAPSED -lt $METADATA_TIMEOUT ]; do
        LIST_RES=$(curl -s "http://localhost:8080/resource/${RESOURCE_ID}/list" || echo "")
        if echo "$LIST_RES" | grep -q '"items"'; then
            ITEMS_COUNT=$(echo "$LIST_RES" | jq '.items | length' 2>/dev/null || echo "0")
            if [ "$ITEMS_COUNT" -gt 0 ]; then
                echo "✓ Metadata resolved! Found $ITEMS_COUNT files."
                METADATA_OK=true
                break
            fi
        fi
        sleep 5
        METADATA_ELAPSED=$((METADATA_ELAPSED + 5))
    done

    if [ "$METADATA_OK" = "false" ]; then
        echo "❌ Error: Failed to retrieve torrent metadata in time. Skipping mode $MODE."
        continue
    fi

    # 7. Trigger Ingestion via Vault API
    echo "Triggering ingestion in Vault..."
    curl -sf -X PUT "http://localhost:8086/resource/$RESOURCE_ID" || {
        echo "❌ Error: Failed to trigger Vault ingestion."
        continue
    }

    echo "Ingestion triggered. Telemetry logging active for 10 minutes..."
    
    # Get initial SSD stats
    if [ -f "$STAT_FILE" ]; then
        read -r r_ios r_merges r_sectors r_ticks w_ios w_merges w_sectors w_ticks remain < "$STAT_FILE"
        init_r_sectors=$r_sectors
        init_w_sectors=$w_sectors
    else
        init_r_sectors=0
        init_w_sectors=0
    fi
    prev_r_sectors=$init_r_sectors
    prev_w_sectors=$init_w_sectors

    # Initialize metrics variables
    cpu_sum=0
    cpu_count=0
    cpu_peak=0
    
    ram_sum=0
    ram_count=0
    ram_peak=0
    
    speed_sum=0
    speed_count=0
    speed_peak=0

    ssd_io_sum=0
    ssd_io_count=0
    ssd_io_peak=0
    
    start_time=$(date +%s)
    last_time=$start_time
    last_size=0
    total_transferred=0

    # Print Telemetry Header
    printf "%-10s | %-12s | %-12s | %-12s | %-10s | %-10s | %-12s\n" "Elapsed" "Inst Speed" "Total Rx" "Sys CPU" "Sys RAM" "Cache Use" "SSD I/O Spd"
    printf "%-10s | %-12s | %-12s | %-12s | %-10s | %-10s | %-12s\n" "--------" "----------" "--------" "-------" "-------" "---------" "------------"

    iterations=$((TEST_DURATION / TELEMETRY_INTERVAL))
    
    for ((iter=1; iter<=iterations; iter++)); do
        # 1s sleep occurs inside get_cpu_use, so we subtract it from the interval sleep
        sleep $((TELEMETRY_INTERVAL - 1))

        cur_time=$(date +%s)
        elapsed_total=$((cur_time - start_time))
        elapsed_inst=$((cur_time - last_time))

        # Query current stored size from Vault API
        cur_size=$(curl -s "http://localhost:8086/resource/$RESOURCE_ID" | jq -r .stored_size 2>/dev/null || echo "0")
        if [ -z "$cur_size" ] || [ "$cur_size" = "null" ]; then
            cur_size=0
        fi

        diff_size=$((cur_size - last_size))
        [ $diff_size -lt 0 ] && diff_size=0
        
        # Inst Speed in MB/s
        if [ $elapsed_inst -gt 0 ]; then
            inst_speed=$(echo "scale=2; $diff_size / 1048576 / $elapsed_inst" | bc)
        else
            inst_speed="0.00"
        fi

        # Total transferred in MB
        total_transferred=$(echo "scale=2; $cur_size / 1048576" | bc)

        # Get current SSD stats
        if [ -f "$STAT_FILE" ]; then
            read -r r_ios r_merges r_sectors r_ticks w_ios w_merges w_sectors w_ticks remain < "$STAT_FILE"
            cur_r_sectors=$r_sectors
            cur_w_sectors=$w_sectors
        else
            cur_r_sectors=0
            cur_w_sectors=0
        fi

        diff_r_sec=$((cur_r_sectors - prev_r_sectors))
        diff_w_sec=$((cur_w_sectors - prev_w_sectors))
        [ $diff_r_sec -lt 0 ] && diff_r_sec=0
        [ $diff_w_sec -lt 0 ] && diff_w_sec=0

        # Calculate instantaneous speed in MB/s
        if [ $elapsed_inst -gt 0 ]; then
            ssd_read_speed=$(echo "scale=2; $diff_r_sec * 512 / 1048576 / $elapsed_inst" | bc)
            ssd_write_speed=$(echo "scale=2; $diff_w_sec * 512 / 1048576 / $elapsed_inst" | bc)
        else
            ssd_read_speed="0.00"
            ssd_write_speed="0.00"
        fi

        prev_r_sectors=$cur_r_sectors
        prev_w_sectors=$cur_w_sectors

        # Calculate combined SSD I/O rate (reads + writes share the same OCI performance ceiling)
        ssd_io_speed=$(echo "scale=2; $ssd_read_speed + $ssd_write_speed" | bc)
        
        # Update SSD sums & peaks
        ssd_io_sum=$(echo "$ssd_io_sum + $ssd_io_speed" | bc)
        ssd_io_count=$((ssd_io_count + 1))
        if (( $(echo "$ssd_io_speed > $ssd_io_peak" | bc -l) )); then
            ssd_io_peak=$ssd_io_speed
        fi

        # Get system metrics
        cpu_pct=$(get_cpu_use)
        ram_used=$(get_ram_use)

        # Update sums & peaks
        # CPU
        cpu_sum=$(echo "$cpu_sum + $cpu_pct" | bc)
        cpu_count=$((cpu_count + 1))
        if (( $(echo "$cpu_pct > $cpu_peak" | bc -l) )); then
            cpu_peak=$cpu_pct
        fi

        # RAM
        ram_sum=$(echo "$ram_sum + $ram_used" | bc)
        ram_count=$((ram_count + 1))
        if (( $(echo "$ram_used > $ram_peak" | bc -l) )); then
            ram_peak=$ram_used
        fi

        # Speed (ignore initial phase if speed is exactly 0)
        speed_sum=$(echo "$speed_sum + $inst_speed" | bc)
        speed_count=$((speed_count + 1))
        if (( $(echo "$inst_speed > $speed_peak" | bc -l) )); then
            speed_peak=$inst_speed
        fi

        # Check loopback RAM / SSD space usage
        if mountpoint -q /mnt/seeder-cache; then
            cache_use=$(df -h /mnt/seeder-cache 2>/dev/null | tail -n 1 | awk '{print $5}')
        else
            cache_mb=$(du -sL -m /mnt/seeder-cache 2>/dev/null | awk '{print $1}' || echo "0")
            cache_use="${cache_mb}M"
        fi

        # Print current row
        printf "%-8s s | %-7s MB/s | %-10s MB | %-8s %% | %-8s MB | %-10s | %-7s MB/s\n" \
               "$elapsed_total" "$inst_speed" "$total_transferred" "$cpu_pct" "$ram_used" "$cache_use" "$ssd_io_speed"

        last_size=$cur_size
        last_time=$cur_time
    done

    # Calculate final averages
    if [ $cpu_count -gt 0 ]; then
        cpu_avg=$(echo "scale=2; $cpu_sum / $cpu_count" | bc)
    else
        cpu_avg="0.00"
    fi

    if [ $ram_count -gt 0 ]; then
        ram_avg=$(echo "scale=2; $ram_sum / $ram_count" | bc)
    else
        ram_avg="0.00"
    fi

    if [ $ssd_io_count -gt 0 ]; then
        ssd_io_avg=$(echo "scale=2; $ssd_io_sum / $ssd_io_count" | bc)
    else
        ssd_io_avg="0.00"
    fi

    # Get final SSD stats
    if [ -f "$STAT_FILE" ]; then
        read -r r_ios r_merges r_sectors r_ticks w_ios w_merges w_sectors w_ticks remain < "$STAT_FILE"
        final_r_sectors=$r_sectors
        final_w_sectors=$w_sectors
    else
        final_r_sectors=0
        final_w_sectors=0
    fi

    total_r_sec=$((final_r_sectors - init_r_sectors))
    total_w_sec=$((final_w_sectors - init_w_sectors))
    [ $total_r_sec -lt 0 ] && total_r_sec=0
    [ $total_w_sec -lt 0 ] && total_w_sec=0

    ssd_read_mb=$(echo "scale=2; $total_r_sec * 512 / 1048576" | bc)
    ssd_write_mb=$(echo "scale=2; $total_w_sec * 512 / 1048576" | bc)

    # Speed average: total bytes transferred over the full test window
    speed_avg=$(echo "scale=2; $total_transferred / $TEST_DURATION" | bc)

    echo "=========================================================================="
    echo "📊 BENCHMARK SUMMARY FOR $MODE"
    echo "  Total Transferred: $total_transferred MB"
    echo "  Speed Avg        : $speed_avg MB/s (Peak: $speed_peak MB/s)"
    echo "  CPU Avg          : $cpu_avg % (Peak: $cpu_peak %)"
    echo "  RAM Avg          : $ram_avg MB (Peak: $ram_peak MB)"
    echo "  SSD I/O Avg      : $ssd_io_avg MB/s (Peak: $ssd_io_peak MB/s)"
    echo "=========================================================================="
    
    # 8. Collect diagnostics logs
    LOG_DIR="/srv/octor/scratch/test/$MODE"
    echo "Saving diagnostic logs to $LOG_DIR..."
    mkdir -p "$LOG_DIR"
    
    # Capture journalctl logs since the start of this test run
    SINCE_TIME=$(date -d "@$start_time" +"%Y-%m-%d %H:%M:%S")
    sudo journalctl --since "$SINCE_TIME" --no-pager | grep "octor-" > "$LOG_DIR/systemd.log" 2>/dev/null || true
    
    # Capture system states
    df -h > "$LOG_DIR/df.log" 2>/dev/null || true
    free -m > "$LOG_DIR/free.log" 2>/dev/null || true
    
    # Capture database states
    docker exec -i octor-postgres psql -U webtor -d vault -c "SELECT * FROM resource WHERE resource_id = '$RESOURCE_ID';" > "$LOG_DIR/db-resource.log" 2>/dev/null || true
    docker exec -i octor-postgres psql -U webtor -d vault -c "SELECT hash, status, total_size, stored_size, upload_id FROM file WHERE hash IN (SELECT file_hash FROM resource_file WHERE resource_id = '$RESOURCE_ID');" > "$LOG_DIR/db-files.log" 2>/dev/null || true

    # Capture Prometheus Metrics from the Web Seeder
    curl -s "http://localhost:53054/metrics" > "$LOG_DIR/seeder-metrics.log" 2>/dev/null || true

    # Append to markdown log
    echo "| $MODE | $speed_avg | $speed_peak | $cpu_avg | $cpu_peak | $ram_avg | $ram_peak | $ssd_io_avg | $ssd_io_peak | $total_transferred |" >> "$LOG_FILE"

    echo "✅ Mode $MODE complete. switch_mode.sh will handle teardown on next iteration."
done

echo "🎉 All benchmarks completed! Results saved to $LOG_FILE"
