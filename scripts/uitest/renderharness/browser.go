// browser.go, everything about getting a real renderer onto the page: serving
// frontend/ over loopback HTTP, finding a Chromium, starting it headless, and
// locating its page target.
//
// Why a static server rather than `wails dev` (which is what the Windows harness
// drives): on Linux there is no wails binary in this container, no GTK3 and no
// WebKit2GTK, so `wails dev` cannot start. frontend/index.html references only
// relative paths and the modules import with absolute paths ("/state.js"), so a
// plain file server rooted at frontend/ serves the application exactly as the
// embedded asset server does. What it does NOT serve is the Go bridge, which is
// the documented boundary of this layer (see probes.js and docs/UITESTING.md).
//
// The server runs IN-PROCESS on an ephemeral port rather than as a separate
// script. That is one fewer process to leak on a failure, no port to collide with
// a parallel CI job, and no "did the server come up yet" poll: net.Listen has
// already succeeded by the time the browser is told the URL.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// serveFrontend starts an HTTP server for dir on 127.0.0.1 and an ephemeral port.
// It returns the base URL and a shutdown function.
func serveFrontend(dir string) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("could not open a loopback port to serve %s: %w", dir, err)
	}

	// No-store on everything: the harness may run twice in a row against edited
	// files, and a cached module would test the previous commit.
	handler := http.FileServer(http.Dir(dir))
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		handler.ServeHTTP(w, r)
	})}
	go func() { _ = server.Serve(listener) }()

	url := "http://" + listener.Addr().String() + "/"
	return url, func() { _ = server.Close() }, nil
}

// findChromium locates a Chromium-family browser, in the order a caller would
// expect: an explicit override first, then the browsers this container ships,
// then PATH.
//
// The container pre-installs Chromium for Playwright and points
// PLAYWRIGHT_BROWSERS_PATH at it, which is why that is the second stop.
func findChromium() (string, error) {
	var tried []string

	if override := os.Getenv("CHROME"); override != "" {
		if isExecutableFile(override) {
			return override, nil
		}
		return "", fmt.Errorf(
			"CHROME is set to %q but that is not an executable file. "+
				"Point CHROME at a Chromium or Chrome binary, or unset it to let the harness search", override)
	}

	if root := os.Getenv("PLAYWRIGHT_BROWSERS_PATH"); root != "" {
		// The install layout is <root>/chromium-<build>/chrome-linux/chrome, with a
		// headless-shell variant beside it. Newest build number wins so a machine
		// with several installs behaves predictably.
		patterns := []string{
			filepath.Join(root, "chromium-*", "chrome-linux", "chrome"),
			filepath.Join(root, "chromium", "chrome-linux", "chrome"),
			filepath.Join(root, "chromium_headless_shell-*", "chrome-linux", "headless_shell"),
		}
		for _, pattern := range patterns {
			matches, _ := filepath.Glob(pattern)
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			for _, m := range matches {
				if isExecutableFile(m) {
					return m, nil
				}
			}
			tried = append(tried, pattern)
		}
	} else {
		tried = append(tried, "$PLAYWRIGHT_BROWSERS_PATH (not set)")
	}

	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
		tried = append(tried, name+" (on PATH)")
	}

	return "", fmt.Errorf(
		"no Chromium-family browser found, so the rendering checks cannot run.\n"+
			"  looked for: %s\n"+
			"  fix:        install Chromium, or set CHROME to an existing Chrome/Chromium binary,\n"+
			"              for example CHROME=/opt/pw-browsers/chromium-1194/chrome-linux/chrome",
		strings.Join(tried, ", "))
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// browser is a running headless Chromium plus the throwaway profile it uses.
type browser struct {
	cmd        *exec.Cmd
	profileDir string
	devtools   string // http://127.0.0.1:<port>
	logPath    string
}

// launchChromium starts a headless Chromium on url and waits for its DevTools
// endpoint to be ready.
//
// --remote-debugging-port=0 lets the OS pick the port and Chromium writes it to
// DevToolsActivePort in the profile directory. That is deliberate: a hardcoded
// 9222 collides the moment two jobs share a runner, and polling a fixed port
// cannot tell "not up yet" from "somebody else's browser".
func launchChromium(exePath, url, artifactDir string) (*browser, error) {
	profileDir, err := os.MkdirTemp("", "doc-anonymiser-uitest-")
	if err != nil {
		return nil, fmt.Errorf("could not create a throwaway browser profile: %w", err)
	}

	args := []string{
		"--headless=new",
		"--remote-debugging-port=0",
		"--remote-allow-origins=*",
		"--user-data-dir=" + profileDir,
		"--window-size=1440,900",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-gpu",
		// This runs as root in a container, where Chromium's sandbox cannot start.
		// It is a throwaway profile loading loopback-only content that we wrote
		// ourselves, so there is nothing here for a sandbox to protect.
		"--no-sandbox",
		"--disable-dev-shm-usage",
		// Determinism, not politeness: every one of these is a background job that
		// could touch the network or move the layout mid-measurement, and the
		// local-only guarantee means this harness must not phone home either.
		"--disable-extensions",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-sync",
		"--metrics-recording-only",
		"--no-pings",
		"--mute-audio",
		"--hide-scrollbars=false", // the layout contract is ABOUT scrollbars
		url,
	}

	cmd := exec.Command(exePath, args...)
	// The browser's own stderr is the only explanation available when it refuses
	// to start, so it goes to a file the caller can point the user at.
	logPath := filepath.Join(artifactDir, "chromium.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("could not create the browser log at %s: %w", logPath, err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		os.RemoveAll(profileDir)
		return nil, fmt.Errorf("could not start %s: %w", exePath, err)
	}

	b := &browser{cmd: cmd, profileDir: profileDir, logPath: logPath}

	port, err := waitForDevToolsPort(profileDir, 60*time.Second)
	if err != nil {
		b.stop()
		logFile.Close()
		tail, _ := os.ReadFile(logPath)
		return nil, fmt.Errorf("%w\n  browser output:\n%s", err, indent(shortenTail(string(tail), 1200)))
	}
	b.devtools = "http://127.0.0.1:" + port
	return b, nil
}

// waitForDevToolsPort polls the DevToolsActivePort file Chromium writes once its
// debugging endpoint is listening. Its first line is the port.
func waitForDevToolsPort(profileDir string, timeout time.Duration) (string, error) {
	path := filepath.Join(profileDir, "DevToolsActivePort")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			if line := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)[0]; line != "" {
				return line, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", fmt.Errorf(
		"the browser never wrote %s, so it never opened its DevTools endpoint within %s.\n"+
			"  fix: run the same binary by hand with --headless=new --remote-debugging-port=0 "+
			"--user-data-dir=/tmp/probe and read what it prints", path, timeout)
}

// pageTarget returns the webSocketDebuggerUrl of the first page target, retrying
// while the browser finishes creating its initial tab.
func (b *browser) pageTarget(timeout time.Duration) (string, error) {
	type target struct {
		Type                 string `json:"type"`
		URL                  string `json:"url"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}

	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(b.devtools + "/json/list")
		if err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		var targets []target
		if err := json.Unmarshal(body, &targets); err != nil {
			lastErr = fmt.Errorf("%s/json/list did not return JSON: %w", b.devtools, err)
			continue
		}
		for _, t := range targets {
			if t.Type == "page" && t.WebSocketDebuggerURL != "" {
				return t.WebSocketDebuggerURL, nil
			}
		}
		lastErr = fmt.Errorf("the browser is running but has no page target yet")
		time.Sleep(200 * time.Millisecond)
	}
	return "", fmt.Errorf(
		"could not find a page target at %s/json/list within %s: %w",
		b.devtools, timeout, lastErr)
}

// stop kills the browser and removes its profile. Kill rather than a polite
// shutdown: it is headless, it holds no user data, and a browser that outlives a
// failed harness run is how a CI job hangs.
func (b *browser) stop() {
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
		_, _ = b.cmd.Process.Wait()
	}
	if b.profileDir != "" {
		_ = os.RemoveAll(b.profileDir)
	}
}

// findRepoRoot walks up from the working directory looking for the module root,
// identified by go.mod AND frontend/index.html together, so a stray go.mod
// somewhere above the checkout cannot be mistaken for it.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	start := dir
	for {
		if fileExists(filepath.Join(dir, "go.mod")) &&
			fileExists(filepath.Join(dir, "frontend", "index.html")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf(
				"could not find the doc-anonymiser checkout above %s (looking for a directory with both "+
					"go.mod and frontend/index.html).\n  fix: run this from inside the repository, or pass "+
					"-frontend <path to the frontend directory>", start)
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func indent(s string) string {
	if s == "" {
		return "    (the browser printed nothing)"
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n")
}

func shortenTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

// hostPlatform is reported in the run header: a rendering result is only
// meaningful alongside the engine that produced it.
func hostPlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
