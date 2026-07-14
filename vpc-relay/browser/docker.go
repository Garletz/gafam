package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	dockerSock       = "/var/run/docker.sock"
	browserContainer = "gafam-browser"
	dockerAPIBase    = "http://localhost"
	defaultImage     = "ghcr.io/garletz/gafam-browser:latest"
)

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

func browserImage() string {
	if u := os.Getenv("BROWSER_IMAGE"); u != "" {
		return u
	}
	return defaultImage
}

func containerState() (running bool, err error) {
	if !dockerSockPresent() {
		return false, fmt.Errorf("docker.sock unavailable — mount /var/run/docker.sock on gafam-api")
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
	if resp.StatusCode == http.StatusNotFound {
		return false, fmt.Errorf("container %s missing", browserContainer)
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
	req, err := http.NewRequest(http.MethodGet, dockerAPIBase+"/containers/"+browserContainer+"/json", nil)
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
			"Memory":     int64(600) * 1024 * 1024,
			"MemorySwap": int64(2) * 1024 * 1024 * 1024,
			"RestartPolicy": map[string]interface{}{
				"Name": "no",
			},
			"NetworkMode": "gafam-net",
			"Binds": []string{
				"/root/gafam_data/browser:/home/browser/data",
			},
			"Tmpfs": map[string]string{
				"/tmp":     "size=128m",
				"/dev/shm": "size=128m",
			},
		},
		"NetworkingConfig": map[string]interface{}{
			"EndpointsConfig": map[string]interface{}{
				"gafam-net": map[string]interface{}{},
			},
		},
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, dockerAPIBase+"/containers/create?name="+url.QueryEscape(browserContainer), bytes.NewReader(payload))
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

func startContainer() error {
	if err := ensureContainer(); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, dockerAPIBase+"/containers/"+browserContainer+"/start", nil)
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
	req, err := http.NewRequest(http.MethodPost, dockerAPIBase+"/containers/"+browserContainer+"/stop?t=10", nil)
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

func browserBaseURL() string {
	if u := os.Getenv("BROWSER_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://gafam-browser:6080"
}

func waitBrowserReady(ctx context.Context) error {
	client := &http.Client{Timeout: 5 * time.Second}
	healthURL := browserBaseURL() + "/vnc.html"
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
