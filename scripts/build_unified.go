package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Service struct {
	Name        string // e.g. "rest-api"
	PkgPath     string // e.g. "github.com/webtor-io/rest-api"
	SubDir      string // e.g. "" or "server"
	TargetPkg   string // e.g. "rest_api"
}

func main() {
	services := []Service{
		{Name: "rest-api", PkgPath: "github.com/webtor-io/rest-api", SubDir: "", TargetPkg: "rest_api"},
		{Name: "web-ui", PkgPath: "github.com/webtor-io/web-ui", SubDir: "", TargetPkg: "web_ui"},
		{Name: "vault", PkgPath: "github.com/webtor-io/vault", SubDir: "", TargetPkg: "vault"},
		{Name: "abuse-store", PkgPath: "github.com/webtor-io/abuse-store", SubDir: "", TargetPkg: "abuse_store"},
		{Name: "claims-provider", PkgPath: "github.com/webtor-io/claims-provider", SubDir: "", TargetPkg: "claims_provider"},
		{Name: "torrent-store", PkgPath: "github.com/webtor-io/torrent-store", SubDir: "", TargetPkg: "torrent_store"},
		{Name: "url-store", PkgPath: "github.com/webtor-io/url-store", SubDir: "", TargetPkg: "url_store"},
		{Name: "video-info", PkgPath: "github.com/webtor-io/video-info", SubDir: "", TargetPkg: "video_info"},
		{Name: "torrent-archiver", PkgPath: "github.com/webtor-io/torrent-archiver", SubDir: "", TargetPkg: "torrent_archiver"},
		{Name: "srt2vtt", PkgPath: "github.com/webtor-io/srt2vtt", SubDir: "", TargetPkg: "srt2vtt"},
		{Name: "content-transcoder", PkgPath: "github.com/webtor-io/content-transcoder", SubDir: "", TargetPkg: "content_transcoder"},
		{Name: "magnet2torrent", PkgPath: "github.com/webtor-io/magnet2torrent/server", SubDir: "server", TargetPkg: "magnet2torrent_server"},
		{Name: "torrent-web-seeder", PkgPath: "github.com/webtor-io/torrent-web-seeder/server", SubDir: "server", TargetPkg: "torrent_web_seeder_server"},
		{Name: "content-prober", PkgPath: "github.com/webtor-io/content-prober/server", SubDir: "server", TargetPkg: "content_prober_server"},
		{Name: "torrent-http-proxy", PkgPath: "github.com/webtor-io/torrent-http-proxy", SubDir: "", TargetPkg: "torrent_http_proxy"},
		{Name: "torrent-web-seeder-cleaner", PkgPath: "github.com/webtor-io/torrent-web-seeder-cleaner", SubDir: "", TargetPkg: "torrent_web_seeder_cleaner"},
		{Name: "s3-gateway", PkgPath: "s3-gateway", SubDir: "", TargetPkg: "s3_gateway"},
		{Name: "sidecar", PkgPath: "github.com/webtor-io/sidecar", SubDir: "", TargetPkg: "sidecar"},
		{Name: "ai-proxy", PkgPath: "octor/ai-proxy", SubDir: "", TargetPkg: "ai_proxy"},
		{Name: "create_nats_stream", PkgPath: "octor/create_nats_stream", SubDir: "", TargetPkg: "create_nats_stream"},
		{Name: "recover_db", PkgPath: "octor/recover_db", SubDir: "", TargetPkg: "recover_db"},
		{Name: "clean_orphans", PkgPath: "octor/clean_orphans", SubDir: "", TargetPkg: "clean_orphans"},
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("❌ Failed to get current directory: %v\n", err)
		os.Exit(1)
	}

	buildDir, err := ioutil.TempDir("", "octor-unified-build-*")
	if err != nil {
		fmt.Printf("❌ Failed to create temp build dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(buildDir)

	fmt.Printf("📁 Created temporary build directory: %s\n", buildDir)

	// 1. Copy go.work and configure it
	goWorkContent, err := ioutil.ReadFile(filepath.Join(cwd, "go.work"))
	if err != nil {
		fmt.Printf("❌ Failed to read go.work: %v\n", err)
		os.Exit(1)
	}

	// Replace paths in go.work to use absolute paths of our copied folders
	lines := strings.Split(string(goWorkContent), "\n")
	var newGoWork []string
	var allFolders []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "./") {
			svcName := strings.TrimPrefix(trimmed, "./")
			allFolders = append(allFolders, svcName)
			newGoWork = append(newGoWork, fmt.Sprintf("\t%s", filepath.Join(buildDir, svcName)))
		} else {
			newGoWork = append(newGoWork, line)
		}
	}
	// Add dispatcher
	newGoWork = insertBeforeClosingParen(newGoWork, fmt.Sprintf("\t%s", filepath.Join(buildDir, "dispatcher")))

	err = ioutil.WriteFile(filepath.Join(buildDir, "go.work"), []byte(strings.Join(newGoWork, "\n")), 0644)
	if err != nil {
		fmt.Printf("❌ Failed to write temp go.work: %v\n", err)
		os.Exit(1)
	}

	// 2. Copy ALL Go modules in workspace to temp build dir
	for _, folder := range allFolders {
		srcPath := filepath.Join(cwd, folder)
		dstPath := filepath.Join(buildDir, folder)
		fmt.Printf("🚀 Copying module %s...\n", folder)
		err := copyDir(srcPath, dstPath)
		if err != nil {
			fmt.Printf("❌ Failed to copy module %s: %v\n", folder, err)
			os.Exit(1)
		}
	}

	// 3. Rename package and main() function for executable services
	for _, svc := range services {
		dstPath := filepath.Join(buildDir, svc.Name)
		mainPkgDir := dstPath
		if svc.SubDir != "" {
			mainPkgDir = filepath.Join(dstPath, svc.SubDir)
		}

		err = renameMainPkg(mainPkgDir, svc.TargetPkg)
		if err != nil {
			fmt.Printf("❌ Failed to rename package for %s: %v\n", svc.Name, err)
			os.Exit(1)
		}
	}

	// 3.5. Patch swagger registrations to prevent Register called twice panics
	err = patchSwaggerDocs(buildDir)
	if err != nil {
		fmt.Printf("❌ Failed to patch swagger docs: %v\n", err)
		os.Exit(1)
	}

	// 4. Create the dispatcher directory and files
	dispDir := filepath.Join(buildDir, "dispatcher")
	err = os.MkdirAll(dispDir, 0755)
	if err != nil {
		fmt.Printf("❌ Failed to create dispatcher dir: %v\n", err)
		os.Exit(1)
	}

	// dispatcher/go.mod
	dispGoMod := fmt.Sprintf("module octor/dispatcher\n\ngo 1.26.3\n")
	err = ioutil.WriteFile(filepath.Join(dispDir, "go.mod"), []byte(dispGoMod), 0644)
	if err != nil {
		fmt.Printf("❌ Failed to write dispatcher/go.mod: %v\n", err)
		os.Exit(1)
	}

	// dispatcher/main.go
	dispMain := generateDispatcherMain(services)
	err = ioutil.WriteFile(filepath.Join(dispDir, "main.go"), []byte(dispMain), 0644)
	if err != nil {
		fmt.Printf("❌ Failed to write dispatcher/main.go: %v\n", err)
		os.Exit(1)
	}

	// 5. Build the unified binary
	binDir := filepath.Join(cwd, "bin")
	os.MkdirAll(binDir, 0755)
	binaryPath := filepath.Join(binDir, "octor")

	fmt.Println("🔨 Compiling unified dispatcher binary (octor)...")
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", binaryPath, ".")
	cmd.Dir = dispDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		fmt.Printf("❌ Failed to compile unified binary: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Unified binary successfully created at %s\n", binaryPath)
}

func insertBeforeClosingParen(lines []string, newLine string) []string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == ")" {
			// Insert before this line
			return append(lines[:i], append([]string{newLine}, lines[i:]...)...)
		}
	}
	return append(lines, newLine)
}

func copyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Skip heavy directories that are not needed for Go build
		if info.IsDir() {
			if rel == "node_modules" || rel == "venv" || rel == "assets/dist" || rel == ".git" || rel == "bin" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), info.Mode())
		}

		// Skip binary files and non-essential build artifacts
		if !info.Mode().IsRegular() {
			return nil
		}
		
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(filepath.Join(dst, rel), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func renameMainPkg(dir string, targetPkg string) error {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") {
			continue
		}

		path := filepath.Join(dir, file.Name())
		content, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}

		contentStr := string(content)

		// Check if it's package main
		// Use exact package declaration matching
		isMainPkg := false
		lines := strings.Split(contentStr, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "package main") {
				isMainPkg = true
				break
			}
		}

		if isMainPkg {
			// Replace package main with package targetPkg
			// We must be careful about comments or formatting
			// Simple replace works fine
			contentStr = strings.Replace(contentStr, "package main", "package "+targetPkg, 1)

			// Replace func main() with func Main()
			contentStr = strings.Replace(contentStr, "func main()", "func Main()", 1)

			err = ioutil.WriteFile(path, []byte(contentStr), file.Mode())
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func generateDispatcherMain(services []Service) string {
	var imports []string
	var mapEntries []string

	for _, svc := range services {
		imports = append(imports, fmt.Sprintf("\t%s %q", svc.TargetPkg, svc.PkgPath))
		mapEntries = append(mapEntries, fmt.Sprintf("\t\t%q: %s.Main,", svc.Name, svc.TargetPkg))
	}

	return fmt.Sprintf(`package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

%s
)

func main() {
	services := map[string]func(){
%s
	}

	arg0 := filepath.Base(os.Args[0])
	arg0 = strings.TrimSuffix(arg0, ".exe")

	if runFn, ok := services[arg0]; ok {
		runFn()
		return
	}

	if len(os.Args) > 1 {
		cmd := os.Args[1]
		if runFn, ok := services[cmd]; ok {
			// Shift args to remove the service name subcommand
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
			runFn()
			return
		}
	}

	fmt.Println("Octor Multi-Call Dispatcher Binary")
	fmt.Println("Usage: octor <service> [args...]")
	fmt.Println("   or: symlink a service name to octor and execute it directly")
	fmt.Println("\nAvailable services:")
	for name := range services {
		fmt.Printf("  - %%s\n", name)
	}
	os.Exit(1)
}
`, strings.Join(imports, "\n"), strings.Join(mapEntries, "\n"))
}

func patchSwaggerDocs(buildDir string) error {
	return filepath.Walk(buildDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Only check if it's in a docs folder
		dir := filepath.Base(filepath.Dir(path))
		if dir != "docs" {
			return nil
		}

		content, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}

		contentStr := string(content)
		if strings.Contains(contentStr, "swag.Register(") {
			// Wrap swag.Register with a recover to prevent duplicate registration panic
			contentStr = strings.ReplaceAll(contentStr, "swag.Register(", "defer func() { recover() }(); swag.Register(")
			err = ioutil.WriteFile(path, []byte(contentStr), info.Mode())
			if err != nil {
				return err
			}
			fmt.Printf("🩹 Patched swagger registration in %s to ignore duplicate registration panic\n", path)
		}
		return nil
	})
}

