package runtime

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

const (
	defaultImage            = "ghcr.io/usestrix/strix-sandbox:0.1.13"
	containerToolServerPort = "48081"
	containerCaidoPort      = "48080"
	hostGatewayHostname     = "host.docker.internal"
)

type LocalSource struct {
	SourcePath      string `json:"source_path"`
	WorkspaceSubdir string `json:"workspace_subdir"`
}

type SandboxInfo struct {
	WorkspaceID    string `json:"workspace_id"`
	APIURL         string `json:"api_url"`
	AuthToken      string `json:"auth_token"`
	ToolServerPort int    `json:"tool_server_port"`
	CaidoPort      int    `json:"caido_port"`
	AgentID        string `json:"agent_id"`
}

type DockerRuntime struct {
	cli *client.Client
}

func NewDockerRuntime() (*DockerRuntime, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}
	return &DockerRuntime{cli: cli}, nil
}

func (r *DockerRuntime) CreateSandbox(ctx context.Context, agentID string, localSources []LocalSource) (*SandboxInfo, error) {
	imageName := os.Getenv("STRIX_IMAGE")
	if imageName == "" {
		imageName = defaultImage
	}

	slog.Debug("Ensuring sandbox docker image is present", slog.String("image", imageName))
	// 1. Verify and pull image if not present
	if err := r.ensureImage(ctx, imageName); err != nil {
		return nil, err
	}

	scanID := fmt.Sprintf("scan-%s", agentID)
	containerName := fmt.Sprintf("strix-scan-%s", scanID)

	slog.Info("Creating sandbox container",
		slog.String("agent_id", agentID),
		slog.String("container_name", containerName),
		slog.String("image", imageName),
	)

	// Clean up existing container with same name if any
	_ = r.cli.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})

	// 2. Select free ports
	toolServerPort, err := getFreePort()
	if err != nil {
		return nil, err
	}
	caidoPort, err := getFreePort()
	if err != nil {
		return nil, err
	}

	// Generate secure token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)

	// 3. Define Container Configuration
	config := &container.Config{
		Image: imageName,
		Cmd:   []string{"sleep", "infinity"},
		Tty:   true,
		Labels: map[string]string{
			"strix-scan-id": scanID,
		},
		Env: []string{
			"PYTHONUNBUFFERED=1",
			"TOOL_SERVER_PORT=" + containerToolServerPort,
			"TOOL_SERVER_TOKEN=" + token,
			"STRIX_SANDBOX_EXECUTION_TIMEOUT=120",
			"HOST_GATEWAY=" + hostGatewayHostname,
		},
	}

	// Port bindings
	portBindings := nat.PortMap{
		nat.Port(containerToolServerPort + "/tcp"): []nat.PortBinding{
			{HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", toolServerPort)},
		},
		nat.Port(containerCaidoPort + "/tcp"): []nat.PortBinding{
			{HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", caidoPort)},
		},
	}

	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		CapAdd:       []string{"NET_ADMIN", "NET_RAW"},
		ExtraHosts:   []string{hostGatewayHostname + ":host-gateway"},
	}

	// 4. Create and start container
	resp, err := r.cli.ContainerCreate(ctx, config, hostConfig, nil, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	slog.Debug("Starting sandbox container", slog.String("container_id", resp.ID))
	err = r.cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	if err != nil {
		// Cleanup container if start fails
		_ = r.cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	apiURL := fmt.Sprintf("http://127.0.0.1:%d", toolServerPort)

	// 5. Wait for the tool server health endpoint
	slog.Info("Waiting for sandbox tool server health check", slog.String("url", apiURL))
	if err := r.waitForToolServer(ctx, apiURL); err != nil {
		_ = r.cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return nil, err
	}
	slog.Info("Sandbox tool server is healthy", slog.String("url", apiURL))

	// 6. Copy local source directories if provided
	for _, src := range localSources {
		slog.Info("Copying local source directory to sandbox container",
			slog.String("container_id", resp.ID),
			slog.String("source_path", src.SourcePath),
			slog.String("workspace_subdir", src.WorkspaceSubdir),
		)
		err = r.copyDirToContainer(ctx, resp.ID, src.SourcePath, src.WorkspaceSubdir)
		if err != nil {
			slog.Warn("Failed to copy source directory to sandbox",
				slog.String("source_path", src.SourcePath),
				slog.Any("error", err),
			)
		}
	}

	// Set permissions
	execResp, err := r.cli.ContainerExecCreate(ctx, resp.ID, container.ExecOptions{
		User: "root",
		Cmd:  []string{"chown", "-R", "pentester:pentester", "/workspace"},
	})
	if err == nil {
		_ = r.cli.ContainerExecStart(ctx, execResp.ID, container.ExecStartOptions{})
	}

	slog.Info("Sandbox container created and initialized successfully",
		slog.String("container_id", resp.ID),
		slog.Int("tool_server_port", toolServerPort),
		slog.Int("caido_port", caidoPort),
	)

	return &SandboxInfo{
		WorkspaceID:    resp.ID,
		APIURL:         apiURL,
		AuthToken:      token,
		ToolServerPort: toolServerPort,
		CaidoPort:      caidoPort,
		AgentID:        agentID,
	}, nil
}

func (r *DockerRuntime) DestroySandbox(ctx context.Context, containerID string) error {
	slog.Info("Destroying sandbox container", slog.String("container_id", containerID))
	stopTimeout := 5
	err := r.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &stopTimeout})
	if err != nil {
		return err
	}
	return r.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

func (r *DockerRuntime) ensureImage(ctx context.Context, imageName string) error {
	_, _, err := r.cli.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		return nil // Image exists locally
	}

	slog.Info("Pulling docker image", slog.String("image", imageName))
	reader, err := r.cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}
	defer reader.Close()

	// Consume pull stream
	_, _ = io.Copy(io.Discard, reader)
	slog.Info("Successfully pulled docker image", slog.String("image", imageName))
	return nil
}

func (r *DockerRuntime) waitForToolServer(ctx context.Context, apiURL string) error {
	healthURL := apiURL + "/health"
	client := &http.Client{Timeout: 2 * time.Second}

	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					// Read response to ensure healthy JSON
					body, _ := io.ReadAll(resp.Body)
					if bytes.Contains(body, []byte(`"status":"healthy"`)) {
						return nil
					}
				}
			}
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("tool server healthcheck timed out on %s", healthURL)
}

func (r *DockerRuntime) copyDirToContainer(ctx context.Context, containerID string, localPath string, targetSubdir string) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	baseDir := filepath.Clean(localPath)
	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}

		// Prepend targetSubdir if provided
		if targetSubdir != "" {
			header.Name = filepath.Join(targetSubdir, relPath)
		} else {
			header.Name = relPath
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.Mode().IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(tw, file)
		return err
	})
	if err != nil {
		return err
	}
	tw.Close()

	return r.cli.CopyToContainer(ctx, containerID, "/workspace", &buf, container.CopyToContainerOptions{})
}

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
