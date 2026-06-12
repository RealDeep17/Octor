package admin

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/web"
)

type ServiceStatus struct {
	Name      string `json:"name"`
	Type      string `json:"type"`      // "database", "core", "external", "mount"
	Status    string `json:"status"`    // "online", "offline", "degraded"
	Message   string `json:"message"`   // Details / errors
	Port      int    `json:"port"`
	ErrorLogs string `json:"error_logs"` // Last few lines of error logs if offline
	Action    string `json:"action"`     // Action code for recovery, e.g. "restart-service-octor-sidecar"
}

type StatusPageData struct {
	CPUUsage    float64
	RAMUsed     int64
	RAMTotal    int64
	RAMPercent  float64
	DiskUsed    int64
	DiskTotal   int64
	DiskPercent float64
	StreamCount int
	SeedCount   int

	// IO Stats (Live Rate MB/s)
	DiskReadRate  float64
	DiskWriteRate float64
	NetRxRate     float64
	NetTxRate     float64

	// IO Diffs (Total last 30 days bytes)
	TotalDiskRead30d  int64
	TotalDiskWrite30d int64
	TotalNetRx30d     int64
	TotalNetTx30d     int64

	// Health services status
	Services []ServiceStatus

	// Recovery / Watcher state
	RecoveryRunning bool
	RecoveryAction  string
	RecoveryLog     string
}

var (
	liveTelemetry      StatusPageData
	liveTelemetryMutex sync.RWMutex
	baseDiskRx         = regexp.MustCompile(`^([sv]d[a-z]+|nvme[0-9]+n[0-9]+)$`)
)

// StartStatsCollector collects network and disk I/O metrics and stores hourly/minutely diffs in the database
func StartStatsCollector(db *pg.DB) {
	if db == nil {
		return
	}

	// 1. Initial 30-day query to populate telemetry on start
	var stats30d struct {
		DiskRead  int64 `pg:"disk_read"`
		DiskWrite int64 `pg:"disk_write"`
		NetRx     int64 `pg:"net_rx"`
		NetTx     int64 `pg:"net_tx"`
	}
	_, err := db.QueryOne(&stats30d, `
		SELECT 
			COALESCE(SUM(disk_read_bytes), 0) as disk_read, 
			COALESCE(SUM(disk_write_bytes), 0) as disk_write, 
			COALESCE(SUM(net_rx_bytes), 0) as net_rx, 
			COALESCE(SUM(net_tx_bytes), 0) as net_tx 
		FROM public.system_io_stats 
		WHERE timestamp > NOW() - INTERVAL '30 days'
	`)
	if err == nil {
		liveTelemetry.TotalDiskRead30d = stats30d.DiskRead
		liveTelemetry.TotalDiskWrite30d = stats30d.DiskWrite
		liveTelemetry.TotalNetRx30d = stats30d.NetRx
		liveTelemetry.TotalNetTx30d = stats30d.NetTx
	}

	// Initialize basic services structure
	liveTelemetry.Services = []ServiceStatus{}

	// 2. Start background telemetry and health collector loops
	go telemetryCollectorLoop()
	go healthCollectorLoop(db)
	go databaseCollectorLoop(db)
}

func telemetryCollectorLoop() {
	var lastDiskRead, lastDiskWrite, lastNetRx, lastNetTx int64
	var lastTime time.Time

	// Initial reads
	lastDiskRead, lastDiskWrite, _ = readDiskStats()
	lastNetRx, lastNetTx, _ = readNetStats()
	lastTime = time.Now()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		elapsed := now.Sub(lastTime).Seconds()
		if elapsed <= 0 {
			elapsed = 3.0
		}

		currDiskRead, currDiskWrite, err1 := readDiskStats()
		currNetRx, currNetTx, err2 := readNetStats()

		var diskReadRate, diskWriteRate, netRxRate, netTxRate float64

		if err1 == nil && lastDiskRead > 0 {
			diffRead := currDiskRead - lastDiskRead
			diffWrite := currDiskWrite - lastDiskWrite
			if diffRead < 0 {
				diffRead = 0
			}
			if diffWrite < 0 {
				diffWrite = 0
			}
			diskReadRate = (float64(diffRead) / 1048576.0) / elapsed
			diskWriteRate = (float64(diffWrite) / 1048576.0) / elapsed
		}

		if err2 == nil && lastNetRx > 0 {
			diffRx := currNetRx - lastNetRx
			diffTx := currNetTx - lastNetTx
			if diffRx < 0 {
				diffRx = 0
			}
			if diffTx < 0 {
				diffTx = 0
			}
			netRxRate = (float64(diffRx) / 1048576.0) / elapsed
			netTxRate = (float64(diffTx) / 1048576.0) / elapsed
		}

		if err1 == nil {
			lastDiskRead = currDiskRead
			lastDiskWrite = currDiskWrite
		}
		if err2 == nil {
			lastNetRx = currNetRx
			lastNetTx = currNetTx
		}
		lastTime = now

		// Query CPU, memory, streams, seeds
		cpu := getCPUUsageVal()
		ramUsed := getRAMUsedVal()
		ramTotal := getRAMTotalVal()
		diskUsed := getDiskUsedVal()
		diskTotal := getDiskTotalVal()
		streamCount := getStreamCountVal()
		seedCount := getSeedCountVal()

		var ramPercent, diskPercent float64
		if ramTotal > 0 {
			ramPercent = float64(ramUsed) / float64(ramTotal) * 100
		}
		if diskTotal > 0 {
			diskPercent = float64(diskUsed) / float64(diskTotal) * 100
		}

		// Save to thread-safe telemetry storage
		liveTelemetryMutex.Lock()
		liveTelemetry.CPUUsage = cpu
		liveTelemetry.RAMUsed = ramUsed
		liveTelemetry.RAMTotal = ramTotal
		liveTelemetry.RAMPercent = ramPercent
		liveTelemetry.DiskUsed = diskUsed
		liveTelemetry.DiskTotal = diskTotal
		liveTelemetry.DiskPercent = diskPercent
		liveTelemetry.DiskReadRate = diskReadRate
		liveTelemetry.DiskWriteRate = diskWriteRate
		liveTelemetry.NetRxRate = netRxRate
		liveTelemetry.NetTxRate = netTxRate
		liveTelemetry.StreamCount = streamCount
		liveTelemetry.SeedCount = seedCount
		liveTelemetryMutex.Unlock()
	}
}

func healthCollectorLoop(db *pg.DB) {
	// Query health checks every 10 seconds in background
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Initial run
	servicesList := checkHealthVal(db)
	liveTelemetryMutex.Lock()
	liveTelemetry.Services = servicesList
	liveTelemetryMutex.Unlock()

	for range ticker.C {
		servicesList := checkHealthVal(db)
		liveTelemetryMutex.Lock()
		liveTelemetry.Services = servicesList
		liveTelemetryMutex.Unlock()
	}
}

func databaseCollectorLoop(db *pg.DB) {
	var lastDiskRead, lastDiskWrite, lastNetRx, lastNetTx int64
	lastDiskRead, lastDiskWrite, _ = readDiskStats()
	lastNetRx, lastNetTx, _ = readNetStats()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		currDiskRead, currDiskWrite, err1 := readDiskStats()
		currNetRx, currNetTx, err2 := readNetStats()
		if err1 != nil || err2 != nil {
			continue
		}

		var diffDiskRead, diffDiskWrite, diffNetRx, diffNetTx int64

		if currDiskRead >= lastDiskRead {
			diffDiskRead = currDiskRead - lastDiskRead
		} else {
			diffDiskRead = currDiskRead
		}
		if currDiskWrite >= lastDiskWrite {
			diffDiskWrite = currDiskWrite - lastDiskWrite
		} else {
			diffDiskWrite = currDiskWrite
		}
		if currNetRx >= lastNetRx {
			diffNetRx = currNetRx - lastNetRx
		} else {
			diffNetRx = currNetRx
		}
		if currNetTx >= lastNetTx {
			diffNetTx = currNetTx - lastNetTx
		} else {
			diffNetTx = currNetTx
		}

		lastDiskRead = currDiskRead
		lastDiskWrite = currDiskWrite
		lastNetRx = currNetRx
		lastNetTx = currNetTx

		_, err := db.Exec(`
			INSERT INTO public.system_io_stats (timestamp, disk_read_bytes, disk_write_bytes, net_rx_bytes, net_tx_bytes)
			VALUES (NOW(), ?, ?, ?, ?)
		`, diffDiskRead, diffDiskWrite, diffNetRx, diffNetTx)
		if err != nil {
			log.WithError(err).Warn("Failed to insert system IO stats")
		}

		// Delete system_io_stats records older than 30 days to prevent metrics database bloat
		if _, errPrune := db.Exec("DELETE FROM public.system_io_stats WHERE timestamp < NOW() - INTERVAL '30 days'"); errPrune != nil {
			log.WithError(errPrune).Warn("Failed to prune system IO stats")
		}

		// Update 30-Day aggregate I/O
		var stats30d struct {
			DiskRead  int64 `pg:"disk_read"`
			DiskWrite int64 `pg:"disk_write"`
			NetRx     int64 `pg:"net_rx"`
			NetTx     int64 `pg:"net_tx"`
		}
		_, err30d := db.QueryOne(&stats30d, `
			SELECT 
				COALESCE(SUM(disk_read_bytes), 0) as disk_read, 
				COALESCE(SUM(disk_write_bytes), 0) as disk_write, 
				COALESCE(SUM(net_rx_bytes), 0) as net_rx, 
				COALESCE(SUM(net_tx_bytes), 0) as net_tx 
			FROM public.system_io_stats 
			WHERE timestamp > NOW() - INTERVAL '30 days'
		`)
		if err30d == nil {
			liveTelemetryMutex.Lock()
			liveTelemetry.TotalDiskRead30d = stats30d.DiskRead
			liveTelemetry.TotalDiskWrite30d = stats30d.DiskWrite
			liveTelemetry.TotalNetRx30d = stats30d.NetRx
			liveTelemetry.TotalNetTx30d = stats30d.NetTx
			liveTelemetryMutex.Unlock()
		}
	}
}

func readDiskStats() (int64, int64, error) {
	file, err := os.Open("/proc/diskstats")
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	var readBytes, writeBytes int64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}
		dev := fields[2]
		if strings.HasPrefix(dev, "loop") || strings.HasPrefix(dev, "ram") || strings.Contains(dev, "dm-") {
			continue
		}
		isBaseDisk := baseDiskRx.MatchString(dev)
		if !isBaseDisk {
			continue
		}
		sectorsRead, _ := strconv.ParseInt(fields[5], 10, 64)
		sectorsWritten, _ := strconv.ParseInt(fields[9], 10, 64)
		readBytes += sectorsRead * 512
		writeBytes += sectorsWritten * 512
	}
	return readBytes, writeBytes, nil
}

func readNetStats() (int64, int64, error) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	var rxBytes, txBytes int64
	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		_ = scanner.Text()
	}
	if scanner.Scan() {
		_ = scanner.Text()
	}
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" || strings.HasPrefix(iface, "docker") || strings.HasPrefix(iface, "br-") || strings.HasPrefix(iface, "veth") {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		rx, _ := strconv.ParseInt(fields[0], 10, 64)
		tx, _ := strconv.ParseInt(fields[8], 10, 64)
		rxBytes += rx
		txBytes += tx
	}
	return rxBytes, txBytes, nil
}

func getHostIP() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return "127.0.0.1"
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 3 && fields[1] == "00000000" {
			ipHex := fields[2]
			if len(ipHex) == 8 {
				b0, _ := strconv.ParseUint(ipHex[6:8], 16, 8)
				b1, _ := strconv.ParseUint(ipHex[4:6], 16, 8)
				b2, _ := strconv.ParseUint(ipHex[2:4], 16, 8)
				b3, _ := strconv.ParseUint(ipHex[0:2], 16, 8)
				return fmt.Sprintf("%d.%d.%d.%d", b0, b1, b2, b3)
			}
		}
	}
	return "127.0.0.1"
}

func readLastLines(path string, maxLines int) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}

func (h *Handler) status(c *gin.Context) {
	// Read cached telemetry under read lock instantly
	liveTelemetryMutex.RLock()
	data := liveTelemetry
	liveTelemetryMutex.RUnlock()

	// Read watcher status and logs dynamically on page request
	statusPath := getInfraDataPath("health-action.status")
	logPath := getInfraDataPath("health-action.log")

	if statusBytes, err := os.ReadFile(statusPath); err == nil {
		statusStr := strings.TrimSpace(string(statusBytes))
		if strings.HasPrefix(statusStr, "running:") {
			data.RecoveryRunning = true
			data.RecoveryAction = strings.TrimPrefix(statusStr, "running:")
		}
	}

	if logBytes, err := os.ReadFile(logPath); err == nil {
		data.RecoveryLog = string(logBytes)
	}

	h.tb.Build("admin/status").HTML(http.StatusOK, web.NewContext(c).WithData(&data))
}

func checkHealthVal(db *pg.DB) []ServiceStatus {
	// Define the internal microservices list
	internalSvcs := []struct {
		name string
		port int
	}{
		{"octor-rest-api", 52080},
		{"octor-vault", 52086},
		{"octor-torrent-web-seeder", 52054},
		{"octor-abuse-store", 52059},
		{"octor-claims-provider", 52060},
		{"octor-content-prober", 52062},
		{"octor-content-transcoder", 52055},
		{"octor-magnet2torrent", 52053},
		{"octor-url-store", 52061},
		{"octor-video-info", 52056},
		{"octor-torrent-archiver", 52057},
		{"octor-srt2vtt", 52058},
		{"octor-torrent-http-proxy", 52052},
		{"octor-sidecar", 8000},
		{"octor-ai-proxy", 3456},
		{"octor-s3-gateway", 9000},
	}

	// External apps list
	externalApps := []struct {
		name string
		port int
	}{
		{"Sonarr", 8989},
		{"Radarr", 7878},
		{"Prowlarr", 9696},
		{"Whisparr", 6969},
		{"Zilean", 8182},
		{"Byparr", 8191},
	}

	totalChecks := 4 + len(internalSvcs) + len(externalApps)
	results := make([]ServiceStatus, totalChecks)
	var wg sync.WaitGroup
	wg.Add(totalChecks)

	// 1. PostgreSQL Database check (Index 0)
	go func() {
		defer wg.Done()
		pgStatus := ServiceStatus{Name: "PostgreSQL Database", Type: "database", Status: "online", Action: "restart-postgres"}
		var one int
		if _, err := db.QueryOne(pg.Scan(&one), "SELECT 1"); err != nil {
			pgStatus.Status = "offline"
			pgStatus.Message = err.Error()
		}
		results[0] = pgStatus
	}()

	// 2. Redis Cache check (Index 1)
	go func() {
		defer wg.Done()
		redisHost := os.Getenv("REDIS_HOST")
		if redisHost == "" {
			redisHost = "octor-redis"
		}
		redisStatus := ServiceStatus{Name: "Redis Cache", Type: "database", Status: "online", Action: "restart-redis"}
		if conn, err := net.DialTimeout("tcp", redisHost+":6379", 500*time.Millisecond); err != nil {
			if conn2, err2 := net.DialTimeout("tcp", "127.0.0.1:6380", 500*time.Millisecond); err2 != nil {
				redisStatus.Status = "offline"
				redisStatus.Message = err2.Error()
			} else {
				conn2.Close()
			}
		} else {
			conn.Close()
		}
		results[1] = redisStatus
	}()

	// 3. NATS Event Broker check (Index 2)
	go func() {
		defer wg.Done()
		natsHost := os.Getenv("NATS_HOST")
		if natsHost == "" {
			natsHost = "octor-nats"
		}
		natsStatus := ServiceStatus{Name: "NATS Event Broker", Type: "database", Status: "online", Action: "restart-nats"}
		if conn, err := net.DialTimeout("tcp", natsHost+":4222", 500*time.Millisecond); err != nil {
			if conn2, err2 := net.DialTimeout("tcp", "127.0.0.1:4222", 500*time.Millisecond); err2 != nil {
				natsStatus.Status = "offline"
				natsStatus.Message = err2.Error()
			} else {
				conn2.Close()
			}
		} else {
			conn.Close()
		}
		results[2] = natsStatus
	}()

	// 4. Google Drive Mount check (Index 3)
	go func() {
		defer wg.Done()
		mountStatus := ServiceStatus{Name: "Google Drive Mount", Type: "mount", Status: "online", Action: "restart-rclone"}
		mountPath := getInfraDataPath("drive-mount-vfs")
		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountPath, &stat); err != nil {
			mountStatus.Status = "offline"
			mountStatus.Message = err.Error()
		} else {
			var rootStat syscall.Statfs_t
			_ = syscall.Statfs("/", &rootStat)
			if stat.Blocks == rootStat.Blocks {
				mountStatus.Status = "offline"
				mountStatus.Message = "Mountpoint directory is local (FUSE mount is not active)"
			}
		}
		results[3] = mountStatus
	}()

	// 5. Internal microservices (Indices 4 to 19)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for idx, s := range internalSvcs {
		resIdx := 4 + idx
		svc := s
		go func(targetIdx int, s struct {
			name string
			port int
		}) {
			defer wg.Done()
			status := ServiceStatus{Name: s.name, Type: "core", Port: s.port, Status: "online", Action: "restart-service-" + s.name}
			url := fmt.Sprintf("http://localhost:%d/liveness", s.port)
			if s.name == "octor-sidecar" || s.name == "octor-ai-proxy" || s.name == "octor-s3-gateway" {
				if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", s.port), 500*time.Millisecond); err != nil {
					status.Status = "offline"
					status.Message = err.Error()
					status.ErrorLogs = readLastLines(filepath.Join("/var/log/supervisor", fmt.Sprintf("%s.err.log", s.name)), 20)
				} else {
					conn.Close()
				}
			} else {
				resp, err := client.Get(url)
				if err != nil || resp.StatusCode != http.StatusOK {
					status.Status = "offline"
					if err != nil {
						status.Message = err.Error()
					} else {
						status.Message = fmt.Sprintf("HTTP %d", resp.StatusCode)
					}
					status.ErrorLogs = readLastLines(filepath.Join("/var/log/supervisor", fmt.Sprintf("%s.err.log", s.name)), 20)
				} else {
					resp.Body.Close()
				}
			}
			results[targetIdx] = status
		}(resIdx, svc)
	}

	// 6. External apps (Indices 20 to 25)
	hostIP := getHostIP()
	for idx, appVal := range externalApps {
		resIdx := 4 + len(internalSvcs) + idx
		app := appVal
		go func(targetIdx int, app struct {
			name string
			port int
		}) {
			defer wg.Done()
			status := ServiceStatus{Name: app.name, Type: "external", Port: app.port, Status: "online", Action: "restart-external-" + strings.ToLower(app.name)}
			if conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", hostIP, app.port), 500*time.Millisecond); err != nil {
				if conn2, err2 := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", app.port), 500*time.Millisecond); err2 != nil {
					status.Status = "offline"
					status.Message = err2.Error()
				} else {
					conn2.Close()
				}
			} else {
				conn.Close()
			}
			results[targetIdx] = status
		}(resIdx, app)
	}

	wg.Wait()
	return results
}

// statusAction writes a trigger command to health-action.trigger to request a host-level self-healing recovery action
func (h *Handler) statusAction(c *gin.Context) {
	action := strings.TrimSpace(c.PostForm("action"))
	if action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "action parameter required"})
		return
	}

	triggerPath := getInfraDataPath("health-action.trigger")
	if err := os.WriteFile(triggerPath, []byte(action), 0644); err != nil {
		log.WithError(err).Error("Failed to write health action trigger file")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create recovery trigger"})
		return
	}

	log.Infof("Admin triggered recovery action: %s", action)
	web.RedirectWithSuccessAndMessage(c, "toast.recoveryTriggered")
}

func getCPUUsageVal() float64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0
	}
	line := scanner.Text()
	parts := strings.Fields(line)
	if len(parts) < 5 || parts[0] != "cpu" {
		return 0
	}
	var total uint64
	for i := 1; i < len(parts); i++ {
		val, _ := strconv.ParseUint(parts[i], 10, 64)
		total += val
	}
	idle, _ := strconv.ParseUint(parts[4], 10, 64)
	time.Sleep(100 * time.Millisecond)
	f2, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f2.Close()
	scanner2 := bufio.NewScanner(f2)
	if !scanner2.Scan() {
		return 0
	}
	parts2 := strings.Fields(scanner2.Text())
	var total2 uint64
	for i := 1; i < len(parts2); i++ {
		val, _ := strconv.ParseUint(parts2[i], 10, 64)
		total2 += val
	}
	idle2, _ := strconv.ParseUint(parts2[4], 10, 64)
	if total2 == total {
		return 0
	}
	return float64(100) * (1 - float64(idle2-idle)/float64(total2-total))
}

func getRAMUsedVal() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	var total, free, buffers, cached uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal: %d", &total)
		} else if strings.HasPrefix(line, "MemFree:") {
			fmt.Sscanf(line, "MemFree: %d", &free)
		} else if strings.HasPrefix(line, "Buffers:") {
			fmt.Sscanf(line, "Buffers: %d", &buffers)
		} else if strings.HasPrefix(line, "Cached:") {
			fmt.Sscanf(line, "Cached: %d", &cached)
		}
	}
	return int64(total-free-buffers-cached) * 1024
}

func getRAMTotalVal() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	var total uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal: %d", &total)
			break
		}
	}
	return int64(total) * 1024
}

func getDiskUsedVal() int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(getInfraDataPath(""), &stat); err != nil {
		return 0
	}
	return int64(stat.Blocks-stat.Bfree) * int64(stat.Bsize)
}

func getDiskTotalVal() int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(getInfraDataPath(""), &stat); err != nil {
		return 0
	}
	return int64(stat.Blocks) * int64(stat.Bsize)
}

func getStreamCountVal() int {
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get("http://localhost:52086/metrics") // query octor-vault probe port
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "vault_worker_leases_held ") {
			fields := strings.Fields(line)
			if len(fields) == 2 {
				val, _ := strconv.Atoi(fields[1])
				return val
			}
		}
	}
	return 0
}

func getSeedCountVal() int {
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get("http://localhost:52054/metrics") // query octor-torrent-web-seeder probe port
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "torrent_web_seeder_active_torrents_count ") {
			fields := strings.Fields(line)
			if len(fields) == 2 {
				val, _ := strconv.Atoi(fields[1])
				return val
			}
		}
	}
	return 0
}
