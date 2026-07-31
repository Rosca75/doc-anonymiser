// Command renderharness is layer 3 of the test strategy (docs/UITESTING.md) on
// Linux: it renders the real frontend in a real Chromium and asserts what a user
// would actually SEE.
//
//	go run ./scripts/uitest/renderharness
//	go run ./scripts/uitest/renderharness -keep-artifacts
//
// # WHY THIS EXISTS
//
// Every one of the seven issues reported against the built application passed the
// existing tests. Go layer 1 proves behaviour through the bound app; node layer 2
// proves the HTML each view builds. Neither can answer a question about PIXELS: is
// this tooltip visible or is its scrolling pane clipping it away, does this screen
// fit the window, did anything log an error on the way. Those are the questions
// here, and they need a renderer.
//
// WHY IT IS A COMMAND AND NOT A `go test`
//
// It needs a Chromium. Folding it into `go test ./...` would either make the
// standard test command fail on a machine without one, or make it skip silently,
// and a gate that silently skips is not a gate. As a command it is explicit: CI
// runs it as its own step, `go vet ./...` and gofmt still cover the code, and
// `go test ./...` stays hermetic and fast.
//
// # WHAT IT DOES NOT COVER
//
// The Go bridge. The page is served as static files, so window.go does not exist
// and every api.js call rejects with the readable error api.js is designed to
// throw. Application state is seeded straight into state.js instead, exactly as
// the Windows harness does. This layer covers RENDERING, not the bridge; the
// bridge belongs to layer 1 (backend/app_e2e_test.go) and to the Windows
// `wails dev` harness, where the bridge is really attached.
//
// Everything it needs ships with Go: net/http for the file server, a small
// RFC 6455 client in ws.go for the DevTools socket, encoding/json for the
// protocol. No new dependency, so nothing to add to the pinned-versions table in
// the root CLAUDE.md.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	frontendDir := flag.String("frontend", "",
		"path to the frontend directory to serve (default: the frontend/ of the checkout above the working directory)")
	probesPath := flag.String("probes", "",
		"path to the shared browser-side probes (default: scripts/uitest/probes.js in the same checkout)")
	artifactDir := flag.String("artifacts", "",
		"where to write the screenshot and the browser log (default: scripts/uitest/artifacts)")
	keepArtifacts := flag.Bool("keep-artifacts", false,
		"keep the screenshot and browser log even when every check passed")
	flag.Parse()

	if err := run(*frontendDir, *probesPath, *artifactDir, *keepArtifacts); err != nil {
		fmt.Fprintf(os.Stderr, "\nrenderharness could not run: %v\n", err)
		os.Exit(2) // 2 = the harness itself failed; 1 = a UI check failed
	}
}

// run does the whole thing and returns an error only for HARNESS failures (no
// browser, no checkout, a crashed page). A failed UI assertion is not an error
// here: it is a result, and it exits 1 further down so the two cases stay
// distinguishable in CI.
func run(frontendDir, probesPath, artifactDir string, keepArtifacts bool) error {
	root, rootErr := findRepoRoot()

	// Each path can be overridden independently, which is what makes the harness
	// runnable from a different working directory or against a copied frontend.
	if frontendDir == "" {
		if rootErr != nil {
			return rootErr
		}
		frontendDir = filepath.Join(root, "frontend")
	}
	if probesPath == "" {
		if rootErr != nil {
			return rootErr
		}
		probesPath = filepath.Join(root, "scripts", "uitest", "probes.js")
	}
	if artifactDir == "" {
		if rootErr != nil {
			return rootErr
		}
		artifactDir = filepath.Join(root, "scripts", "uitest", "artifacts")
	}

	if !fileExists(filepath.Join(frontendDir, "index.html")) {
		return fmt.Errorf(
			"no index.html in %s, so there is no application to render.\n"+
				"  fix: pass -frontend <path to the frontend directory>", frontendDir)
	}
	probes, err := os.ReadFile(probesPath)
	if err != nil {
		return fmt.Errorf(
			"could not read the shared probes at %s: %w\n"+
				"  fix: pass -probes <path to scripts/uitest/probes.js>. Both harnesses read that one file, "+
				"so it is not optional", probesPath, err)
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return fmt.Errorf("could not create the artefact directory %s: %w", artifactDir, err)
	}

	exePath, err := findChromium()
	if err != nil {
		return err
	}

	baseURL, stopServer, err := serveFrontend(frontendDir)
	if err != nil {
		return err
	}
	defer stopServer()

	fmt.Printf("renderharness (%s)\n", hostPlatform())
	fmt.Printf("  frontend: %s\n", frontendDir)
	fmt.Printf("  served:   %s\n", baseURL)
	fmt.Printf("  browser:  %s\n", exePath)
	fmt.Printf("  probes:   %s\n", probesPath)

	b, err := launchChromium(exePath, baseURL, artifactDir)
	if err != nil {
		return err
	}
	defer b.stop()

	wsURL, err := b.pageTarget(30 * time.Second)
	if err != nil {
		return err
	}

	client, err := cdpConnect(wsURL)
	if err != nil {
		return err
	}
	defer client.close()

	// The application boots from an ES module, so the shell exists a tick or two
	// after the document is ready. Polling for the shell it renders is the only
	// honest readiness signal: a load event says the HTML arrived, not that main.js
	// finished.
	if err := waitForShell(client, 30*time.Second); err != nil {
		return err
	}

	// Install the shared probes. This is the ONE place the two harnesses meet: the
	// same file, evaluated the same way, so an assertion is never spelled twice
	// (docs/UITESTING.md).
	var installed string
	if err := client.eval(string(probes), &installed); err != nil {
		return fmt.Errorf("the shared probes at %s would not install: %w", probesPath, err)
	}
	if installed != "installed" {
		return fmt.Errorf(
			"the shared probes at %s returned %q instead of \"installed\", so they may not have run.\n"+
				"  fix: the file must be ONE expression that ends by returning the string \"installed\"",
			probesPath, installed)
	}

	var fx fixture
	if err := client.eval("__uiProbes.fixture()", &fx); err != nil {
		return fmt.Errorf("the probes installed but __uiProbes.fixture() did not answer: %w", err)
	}

	r := &reporter{}
	checkLayout(client, r)
	checkImportPreview(client, r, fx)
	checkConfigureRail(client, r, fx)
	checkTooltip(client, r, fx)
	// Last, so it sees everything every other check provoked.
	checkNoConsoleErrors(client, r)

	// A screenshot of whatever the last check left on screen. It costs a few
	// milliseconds and it is the difference between "the tooltip check failed" and
	// seeing why.
	shotPath := filepath.Join(artifactDir, "wizard.png")
	if png, err := client.screenshot(); err == nil {
		if err := os.WriteFile(shotPath, png, 0o644); err == nil {
			fmt.Printf("\n  screenshot: %s\n", shotPath)
		}
	}

	fmt.Println()
	if len(r.failures) > 0 {
		fmt.Printf("%d of %d rendering check(s) failed:\n", len(r.failures), len(r.failures)+r.passes)
		for _, f := range r.failures {
			fmt.Printf("  - %s\n", f)
		}
		fmt.Printf("Screenshot and browser log are in %s\n", artifactDir)
		// Deliberately not an error return: the harness worked, the UI did not.
		stopServer()
		b.stop()
		os.Exit(1)
	}

	fmt.Printf("All %d rendering checks passed.\n", r.passes)
	if !keepArtifacts {
		// Nothing to look at when everything passed, and a stale screenshot from a
		// green run is worse than none: it is the one somebody reads next time.
		_ = os.Remove(shotPath)
		_ = os.Remove(b.logPath)
	}
	return nil
}

// waitForShell polls until main.js has painted the application shell.
func waitForShell(c *cdpClient, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		var ready struct {
			Shell bool   `json:"shell"`
			Why   string `json:"why"`
		}
		expr := `(() => {
      const app = document.getElementById('app');
      if (!app) return { shell: false, why: 'no #app element: index.html did not load' };
      if (!app.querySelector('.topbar')) return { shell: false, why: 'main.js has not painted the shell yet' };
      return { shell: true, why: '' };
    })()`
		if err := c.eval(expr, &ready); err != nil {
			last = err.Error()
		} else if ready.Shell {
			return nil
		} else {
			last = ready.Why
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf(
		"the application shell never appeared within %s (%s).\n"+
			"  fix: open the served URL in a browser by hand. A module that fails to import stops boot() "+
			"dead, and the console says which one", timeout, last)
}
