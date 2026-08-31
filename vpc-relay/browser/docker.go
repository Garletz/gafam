package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	dockerSock        = "/var/run/docker.sock"
	browserContainer  = "gafam-browser"
	mcpContainer      = "gafam-mcp"
	dockerAPIBase     = "http://localhost"
	defaultImage      = "ghcr.io/garletz/gafam:browser"
	defaultMcpImage   = "ghcr.io/garletz/gafam:mcp"
	// Static IPs on gafam-net: Chrome's DevTools server only accepts Host
	// headers that are IP addresses (DNS-rebinding protection), so the MCP
	// sidecar must address the browser by IP, not by docker name.
	browserStaticIP = "172.18.0.10"
	mcpStaticIP     = "172.18.0.11"
	// The API container mounts /root/gafam_data at /app/data, so the browser
	// profile volume (/root/gafam_data/browser) is directly reachable here.
	browserProfileHost = "/app/data/browser"
)

// The single browser engine is Chrome for Testing, headed on Xvfb, shared by
// the human (MJPEG stream) and the agent (CDP attach via the Playwright MCP
// sidecar). Legacy engine/profile parameters are accepted but ignored.
func browserEngine() string { return "chrome" }

func activeContainer() string { return browserContainer }

func browserImage() string {
	if u := os.Getenv("BROWSER_IMAGE"); u != "" {
		return u
	}
	return defaultImage
}

func browserBaseURL() string {
	return "http://" + browserContainer + ":6080"
}

func dockerHTTP() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", dockerSock)
			},
		},
	}
}

func dockerSockPresent() bool {
	st, err := os.Stat(dockerSock)
	return err == nil && st.Mode()&os.ModeSocket != 0
}

func containerState() (running bool, err error) {
	if !dockerSockPresent() {
		return false, fmt.Errorf("docker.sock unavailable — mount /var/run/docker.sock on gafam-api")
	}
	req, err := http.NewRequest(http.MethodGet, dockerAPIBase+"/containers/"+activeContainer()+"/json", nil)
	if err != nil {
		return false, err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return false, fmt.Errorf("container %s missing", activeContainer())
	}
	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("docker inspect: %s", strings.TrimSpace(string(body)))
	}
	var info struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return false, err
	}
	return info.State.Running, nil
}

func containerExists() (bool, error) {
	if !dockerSockPresent() {
		return false, fmt.Errorf("docker.sock unavailable — mount /var/run/docker.sock on gafam-api")
	}
	req, err := http.NewRequest(http.MethodGet, dockerAPIBase+"/containers/"+activeContainer()+"/json", nil)
	if err != nil {
		return false, err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("docker inspect: %s", strings.TrimSpace(string(body)))
	}
	return true, nil
}

// ensureContainer pulls the public GHCR image and creates gafam-browser if missing.
func ensureContainer() error {
	exists, err := containerExists()
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	img := browserImage()
	if err := pullImage(img); err != nil {
		return fmt.Errorf("pull %s: %w", img, err)
	}
	if err := createContainer(img); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	return nil
}

func pullImage(image string) error {
	// fromImage=repo, tag=latest (Docker Engine API)
	from := image
	tag := "latest"
	if i := strings.LastIndex(image, ":"); i > 0 && !strings.Contains(image[i+1:], "/") {
		from = image[:i]
		tag = image[i+1:]
	}
	q := url.Values{}
	q.Set("fromImage", from)
	q.Set("tag", tag)
	req, err := http.NewRequest(http.MethodPost, dockerAPIBase+"/images/create?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Stream must be consumed (progress JSON lines).
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	if bytes.Contains(body, []byte(`"error"`)) {
		return fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	return nil
}

func createContainer(image string) error {
	cfg := map[string]interface{}{
		"Image": image,
		"HostConfig": map[string]interface{}{
			// Chrome with a few tabs needs ~1-1.5 GB; swap headroom covers peaks.
			"Memory":     int64(1500) * 1024 * 1024,
			"MemorySwap": int64(3) * 1024 * 1024 * 1024,
			"RestartPolicy": map[string]interface{}{
				"Name": "no",
			},
			"NetworkMode": "gafam-net",
			"Binds": []string{
				"/root/gafam_data/browser:/home/browser/data",
			},
			"Tmpfs": map[string]string{
				"/tmp":     "size=256m",
				"/dev/shm": "size=256m",
			},
		},
		"NetworkingConfig": map[string]interface{}{
			"EndpointsConfig": map[string]interface{}{
				"gafam-net": map[string]interface{}{
					"IPAMConfig": map[string]interface{}{
						"IPv4Address": browserStaticIP,
					},
				},
			},
		},
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, dockerAPIBase+"/containers/create?name="+url.QueryEscape(activeContainer()), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusConflict {
		// Already exists — fine.
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	return nil
}

func removeContainer() error {
	req, err := http.NewRequest(http.MethodDelete, dockerAPIBase+"/containers/"+activeContainer()+"?force=true", nil)
	if err != nil {
		return err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("docker rm: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

// EnsureReady wakes the browser sidecar the reliable way — the same path the
// dashboard Wake button uses: recreate (pull + rm + create on gafam-net) if
// it's stale/off-network, start, then wait for /status. Agent tools
// (browser.sense, browser.fetch, …) MUST call this instead of a bare GET —
// a bare GET hits a sleeping or off-network container and fails DNS.
func EnsureReady(ctx context.Context) error {
	if !mu.TryLock() {
		return fmt.Errorf("browser_busy: another wake in progress")
	}
	defer mu.Unlock()

	running, err := containerState()
	if err == nil && running {
		if streamBackendReady() {
			return nil
		}
	}
	if err := startContainer(); err != nil {
		return err
	}
	return waitBrowserReady(ctx)
}

// recreateContainer pulls :browser and replaces the container so tag updates apply.
func recreateContainer() error {
	img := browserImage()
	if err := pullImage(img); err != nil {
		return fmt.Errorf("pull %s: %w", img, err)
	}
	return recreateContainerWithImage(img)
}

// recreateContainerWithImage stops and replaces the container WITHOUT pulling.
// The profile lives on the host volume, so it survives the replacement.
func recreateContainerWithImage(img string) error {
	_ = stopContainer()
	if err := removeContainer(); err != nil {
		return fmt.Errorf("rm: %w", err)
	}
	if err := createContainer(img); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	return nil
}

func startContainer() error {
	img := browserImage()
	if err := pullImage(img); err != nil {
		return fmt.Errorf("pull %s: %w", img, err)
	}
	exists, err := containerExists()
	if err != nil {
		return err
	}
	if exists {
		// Persistence first: only replace the container when a newer image tag
		// was pulled, or its static IP drifted (Chrome's DevTools server only
		// accepts IP-shaped Host headers, so the IP must stay deterministic).
		// Plain start/stop keeps the profile (cookies, logins, localStorage).
		stale, err := containerImageOutdated(img)
		if err == nil && stale {
			log.Println("browser: new image available — recreating container (profile kept on volume)")
			if err := recreateContainerWithImage(img); err != nil {
				return err
			}
		} else if ipErr := browserIPCurrent(); ipErr != nil {
			log.Println("browser: static IP drifted — recreating container")
			if err := recreateContainerWithImage(img); err != nil {
				return err
			}
		}
	} else {
		if err := createContainer(img); err != nil {
			return fmt.Errorf("create: %w", err)
		}
	}
	return dockerStart(browserContainer)
}

// containerImageOutdated reports whether the existing container runs an older
// image than the locally pulled tag (CI publishes new tags; wake applies them).
func containerImageOutdated(image string) (bool, error) {
	localID, err := imageID(image)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequest(http.MethodGet, dockerAPIBase+"/containers/"+browserContainer+"/json", nil)
	if err != nil {
		return false, err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("inspect: %s", strings.TrimSpace(string(body)))
	}
	var info struct {
		Image string `json:"Image"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return false, err
	}
	return strings.TrimPrefix(localID, "sha256:") != strings.TrimPrefix(info.Image, "sha256:"), nil
}

// imageID returns the local image ID for a name:tag reference.
func imageID(image string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, dockerAPIBase+"/images/"+image+"/json", nil)
	if err != nil {
		return "", err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("image %s: %s", image, strings.TrimSpace(string(body)))
	}
	var info struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", err
	}
	return info.ID, nil
}

// browserIPCurrent returns an error when the browser container's IP on
// gafam-net is not the expected static IP (Chrome's DevTools server requires
// IP-shaped Host headers, and the MCP sidecar addresses it by that IP).
func browserIPCurrent() error {
	req, err := http.NewRequest(http.MethodGet, dockerAPIBase+"/containers/"+browserContainer+"/json", nil)
	if err != nil {
		return err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("inspect: %s", strings.TrimSpace(string(body)))
	}
	var info struct {
		NetworkSettings struct {
			Networks map[string]struct {
				IPAddress string `json:"IPAddress"`
			} `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return err
	}
	if netw, ok := info.NetworkSettings.Networks["gafam-net"]; ok && netw.IPAddress == browserStaticIP {
		return nil
	}
	return fmt.Errorf("browser container IP is not %s", browserStaticIP)
}

func dockerStart(name string) error {
	req, err := http.NewRequest(http.MethodPost, dockerAPIBase+"/containers/"+name+"/start", nil)
	if err != nil {
		return err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("docker start: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func stopContainer() error {
	req, err := http.NewRequest(http.MethodPost, dockerAPIBase+"/containers/"+activeContainer()+"/stop?t=10", nil)
	if err != nil {
		return err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("docker stop: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func waitBrowserReady(ctx context.Context) error {
	client := &http.Client{Timeout: 5 * time.Second}
	healthURL := browserBaseURL() + "/status"
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		if browserHealthOK(ctx, client, healthURL) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("browser ready timeout: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func browserHealthOK(ctx context.Context, client *http.Client, healthURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
