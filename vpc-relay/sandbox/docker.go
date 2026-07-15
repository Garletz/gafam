package sandbox

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
	dockerSock        = "/var/run/docker.sock"
	sandboxContainer  = "gafam-sandbox"
	dockerAPIBase     = "http://localhost"
	defaultSandboxImg = "ghcr.io/garletz/gafam:sandbox"
	sandboxHTTPPort   = "6091"
	sandboxWSPort     = "6090"
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

func sandboxImage() string {
	if u := os.Getenv("SANDBOX_IMAGE"); u != "" {
		return u
	}
	return defaultSandboxImg
}

func containerState() (running bool, err error) {
	if !dockerSockPresent() {
		return false, fmt.Errorf("docker.sock unavailable")
	}
	req, err := http.NewRequest(http.MethodGet, dockerAPIBase+"/containers/"+sandboxContainer+"/json", nil)
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
		return false, fmt.Errorf("container %s missing", sandboxContainer)
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
		return false, fmt.Errorf("docker.sock unavailable")
	}
	req, err := http.NewRequest(http.MethodGet, dockerAPIBase+"/containers/"+sandboxContainer+"/json", nil)
	if err != nil {
		return false, err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode != http.StatusNotFound, nil
}

func pullImage(image string) error {
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
			"Memory":     int64(128) * 1024 * 1024,
			"MemorySwap": int64(256) * 1024 * 1024,
			"RestartPolicy": map[string]interface{}{
				"Name": "no",
			},
			"NetworkMode": "gafam-net",
			"Binds": []string{
				"/root/gafam_data/sandbox/files:/sandbox/files",
				"/root/gafam_data/sandbox/downloads:/sandbox/downloads",
				"/root/gafam_data/sandbox/screenshots:/sandbox/screenshots",
			},
			"Tmpfs": map[string]string{
				"/sandbox/tmp": "size=64m",
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
	req, err := http.NewRequest(http.MethodPost, dockerAPIBase+"/containers/create?name="+url.QueryEscape(sandboxContainer), bytes.NewReader(payload))
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
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	return nil
}

func removeContainer() error {
	req, err := http.NewRequest(http.MethodDelete, dockerAPIBase+"/containers/"+sandboxContainer+"?force=true", nil)
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

// recreateContainer pulls :sandbox from public GHCR and replaces the container.
func recreateContainer() error {
	img := sandboxImage()
	if err := pullImage(img); err != nil {
		return fmt.Errorf("pull %s: %w", img, err)
	}
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
	if err := recreateContainer(); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, dockerAPIBase+"/containers/"+sandboxContainer+"/start", nil)
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
	req, err := http.NewRequest(http.MethodPost, dockerAPIBase+"/containers/"+sandboxContainer+"/stop?t=5", nil)
	if err != nil {
		return err
	}
	resp, err := dockerHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return nil
}

func sandboxHTTPURL() string {
	if u := os.Getenv("SANDBOX_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://gafam-sandbox:" + sandboxHTTPPort
}

func sandboxWSURL() string {
	return "http://gafam-sandbox:" + sandboxWSPort
}

func waitSandboxReady(ctx context.Context) error {
	client := &http.Client{Timeout: 5 * time.Second}
	healthURL := sandboxHTTPURL() + "/storage"
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("sandbox ready timeout: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
