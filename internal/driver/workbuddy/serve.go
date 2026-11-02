package workbuddy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const requestHeader = "x-codebuddy-request"
const requestHeaderValue = "agent-mesh"

type meshServe struct {
	gateway
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func (d *Driver) ensureGateway(ctx context.Context, cwd, resume string) (gateway, error) {
	if gws := d.discover(); len(gws) > 0 {
		gw := pickGateway(gws, cwd, resume)
		if err := pingGateway(ctx, gw); err == nil {
			return gw, nil
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.serve != nil && d.serve.cmd != nil && d.serve.cmd.Process != nil {
		if pingGateway(ctx, d.serve.gateway) == nil {
			if resume == "" || d.serve.Session == "" || d.serve.Session == resume {
				return d.serve.gateway, nil
			}
		}
		d.stopServeLocked()
	}
	gw, cmd, cancel, err := startMeshServe(ctx, d.root, cwd, resume)
	if err != nil {
		return gateway{}, err
	}
	d.serve = &meshServe{gateway: gw, cmd: cmd, cancel: cancel}
	return gw, nil
}

func pickGateway(gws []gateway, cwd, resume string) gateway {
	if resume != "" {
		for _, gw := range gws {
			if gw.Session == resume {
				return gw
			}
		}
	}
	if cwd != "" {
		want := filepath.Clean(cwd)
		for _, gw := range gws {
			if gw.CWD != "" && filepath.Clean(gw.CWD) == want {
				return gw
			}
		}
	}
	return gws[0]
}

func pingGateway(ctx context.Context, gw gateway) error {
	if gw.URL == "" {
		return fmt.Errorf("empty gateway url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gw.URL+"/api/v1/health", nil)
	if err != nil {
		return err
	}
	setGatewayHeaders(req, gw)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("health http %d", resp.StatusCode)
	}
	return nil
}

func setGatewayHeaders(req *http.Request, gw gateway) {
	req.Header.Set(requestHeader, requestHeaderValue)
	req.Header.Set("Accept", "application/json, text/event-stream")
	if gw.Password != "" {
		req.Header.Set("Authorization", "Bearer "+gw.Password)
	}
}

func (d *Driver) stopServeLocked() {
	if d.serve == nil {
		return
	}
	if d.serve.cancel != nil {
		d.serve.cancel()
	}
	if d.serve.cmd != nil && d.serve.cmd.Process != nil {
		_ = d.serve.cmd.Process.Kill()
		_, _ = d.serve.cmd.Process.Wait()
	}
	d.serve = nil
}

func startMeshServe(ctx context.Context, root, cwd, resume string) (gateway, *exec.Cmd, context.CancelFunc, error) {
	nodeBin, cliBin, err := resolveWorkBuddyBins(root)
	if err != nil {
		return gateway{}, nil, nil, err
	}
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil && wd != "" {
			cwd = wd
		} else {
			cwd = root
		}
	}
	port, err := freePort()
	if err != nil {
		return gateway{}, nil, nil, err
	}
	args := []string{
		cliBin,
		"--serve",
		"--port", strconv.Itoa(port),
		"--auth=none",
		"--permission-mode", "bypassPermissions",
	}
	if resume != "" {
		args = append(args, "--session-id", resume, "--resume", resume)
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	cmd := exec.CommandContext(runCtx, nodeBin, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "CODEBUDDY_FORCE_LITE_WB_BUNDLE=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return gateway{}, nil, nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		cancel()
		return gateway{}, nil, nil, fmt.Errorf("start workbuddy serve: %w", err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	readyCtx, readyCancel := context.WithTimeout(runCtx, 20*time.Second)
	defer readyCancel()
	if err := waitGatewayReady(readyCtx, url, stdout); err != nil {
		cancel()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return gateway{}, nil, nil, err
	}
	gw := gateway{
		PID:     cmd.Process.Pid,
		URL:     url,
		Session: resume,
		CWD:     cwd,
	}
	go func() {
		_ = cmd.Wait()
	}()
	return gw, cmd, cancel, nil
}

func waitGatewayReady(ctx context.Context, url string, stdout io.Reader) error {
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if strings.Contains(line, "Endpoint") && strings.Contains(line, url) {
				return
			}
			if strings.Contains(line, "Authentication is DISABLED") {
				return
			}
		}
	}()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := pingGateway(ctx, gateway{URL: url}); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("workbuddy serve not ready: %w", ctx.Err())
		case <-done:
			// keep polling until timeout; process may still be binding
		case <-ticker.C:
		}
	}
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func resolveWorkBuddyBins(root string) (nodeBin, cliBin string, err error) {
	if v := strings.TrimSpace(os.Getenv("WORKBUDDY_NODE")); v != "" {
		nodeBin = v
	}
	if v := strings.TrimSpace(os.Getenv("WORKBUDDY_CLI")); v != "" {
		cliBin = v
	}
	if v := strings.TrimSpace(os.Getenv("CODEBUDDY_CLI")); v != "" && cliBin == "" {
		cliBin = v
	}
	if cliBin == "" {
		cliBin = findCodebuddy(root)
	}
	if nodeBin == "" {
		nodeBin = findWorkBuddyNode(root)
	}
	if cliBin == "" {
		return "", "", fmt.Errorf("workbuddy CLI not found (set WORKBUDDY_CLI)")
	}
	if nodeBin == "" {
		if p, lookErr := exec.LookPath("node"); lookErr == nil {
			nodeBin = p
		}
	}
	if nodeBin == "" {
		return "", "", fmt.Errorf("node binary not found (set WORKBUDDY_NODE)")
	}
	return nodeBin, cliBin, nil
}

func findCodebuddy(root string) string {
	var cands []string
	for _, exe := range sniffWorkBuddyInstallDirs() {
		cands = append(cands,
			filepath.Join(exe, "resources", "app.asar.unpacked", "cli", "bin", "codebuddy"),
		)
	}
	cands = append(cands,
		filepath.Join(root, "cli", "bin", "codebuddy"),
		filepath.Join(root, "bin", "codebuddy"),
	)
	if runtime.GOOS == "windows" {
		extra := make([]string, 0, len(cands)*2)
		for _, p := range cands {
			extra = append(extra, p+".cmd", p+".exe")
		}
		cands = append(cands, extra...)
	}
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("codebuddy"); err == nil {
		return p
	}
	return ""
}

func findWorkBuddyNode(root string) string {
	base := filepath.Join(root, "binaries", "node", "versions")
	ents, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	var best string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(base, e.Name(), "node")
		if runtime.GOOS == "windows" {
			p += ".exe"
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			best = p
		}
	}
	return best
}

func workbuddyInstallDir() string {
	dirs := sniffWorkBuddyInstallDirs()
	if len(dirs) == 0 {
		return ""
	}
	return dirs[0]
}
