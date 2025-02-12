//go:build !freebsd

package testutil

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type TestLogConsumer struct {
	Msgs []string
}

func (g *TestLogConsumer) Accept(l testcontainers.Log) {
	g.Msgs = append(g.Msgs, string(l.Content))
}

type Container struct {
	Entrypoint         []string
	Env                map[string]string
	Files              map[string]string
	HostAccessPorts    []int
	HostConfigModifier func(*container.HostConfig)
	ExposedPorts       []string
	Cmd                []string
	Image              string
	Name               string
	Hostname           string
	Networks           []string
	WaitingFor         wait.Strategy

	Address string
	Ports   map[string]string
	Logs    TestLogConsumer

	container testcontainers.Container
	ctx       context.Context
}

func (c *Container) Start() error {
	c.ctx = context.Background()

	files := make([]testcontainers.ContainerFile, 0, len(c.Files))
	for k, v := range c.Files {
		files = append(files, testcontainers.ContainerFile{
			ContainerFilePath: k,
			HostFilePath:      v,
			FileMode:          0o755,
		})
	}

	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Entrypoint:         c.Entrypoint,
			Env:                c.Env,
			ExposedPorts:       c.ExposedPorts,
			Files:              files,
			HostAccessPorts:    c.HostAccessPorts,
			HostConfigModifier: c.HostConfigModifier,
			Cmd:                c.Cmd,
			Image:              c.Image,
			Name:               c.Name,
			Hostname:           c.Hostname,
			Networks:           c.Networks,
			WaitingFor:         c.WaitingFor,
		},
		Started: true,
	}

	cntnr, err := testcontainers.GenericContainer(c.ctx, req)
	if err != nil {
		return fmt.Errorf("container failed to start: %w", err)
	}
	c.container = cntnr

	c.Logs = TestLogConsumer{}
	c.container.FollowOutput(&c.Logs)
	if err := c.container.StartLogProducer(c.ctx); err != nil {
		return fmt.Errorf("log producer failed: %w", err)
	}

	c.Address = "localhost"

	info, err := c.GetInfo()
	if err != nil {
		return fmt.Errorf("getting info failed: %w", err)
	}
	fmt.Println("Started container:", info)

	if err := c.LookupMappedPorts(); err != nil {
		c.Terminate()
		return fmt.Errorf("port lookup failed: %w", err)
	}

	return nil
}

// LookupMappedPorts creates a lookup table of exposed ports to mapped ports
func (c *Container) LookupMappedPorts() error {
	if len(c.ExposedPorts) == 0 {
		return nil
	}

	if len(c.Ports) == 0 {
		c.Ports = make(map[string]string)
	}

	for _, port := range c.ExposedPorts {
		// strip off leading host port: 80:8080 -> 8080
		if strings.Contains(port, ":") {
			port = strings.Split(port, ":")[1]
		}

		p, err := c.container.MappedPort(c.ctx, nat.Port(port))
		if err != nil {
			return fmt.Errorf("failed to find %q: %w", port, err)
		}

		// strip off the transport: 80/tcp -> 80
		if strings.Contains(port, "/") {
			port = strings.Split(port, "/")[0]
		}

		fmt.Printf("mapped container port %q to host port %q\n", port, p.Port())
		c.Ports[port] = p.Port()
	}

	return nil
}

func (c *Container) Exec(cmds []string) (int, io.Reader, error) {
	return c.container.Exec(c.ctx, cmds)
}

func (c *Container) PrintLogs() {
	fmt.Println("--- Container Logs Start ---")
	for _, msg := range c.Logs.Msgs {
		fmt.Print(msg)
	}
	fmt.Println("--- Container Logs End ---")
}

func (c *Container) Terminate() {
	if err := c.container.StopLogProducer(); err != nil {
		fmt.Println(err)
	}

	if err := c.container.Terminate(c.ctx); err != nil {
		fmt.Printf("failed to terminate the container: %s", err)
	}
	c.PrintLogs()
}

func (c *Container) Pause() error {
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return fmt.Errorf("getting provider failed: %w", err)
	}

	return provider.Client().ContainerPause(c.ctx, c.container.GetContainerID())
}

func (c *Container) Resume() error {
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return fmt.Errorf("getting provider failed: %w", err)
	}

	return provider.Client().ContainerUnpause(c.ctx, c.container.GetContainerID())
}

func (c *Container) GetInfo() (string, error) {
	dc, ok := c.container.(*testcontainers.DockerContainer)
	if !ok {
		return "not a docker container", nil
	}

	ci, err := dc.Inspect(c.ctx)
	if err != nil {
		return "", fmt.Errorf("inspecting container failed: %w", err)
	}

	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return "", fmt.Errorf("getting provider failed: %w", err)
	}

	summaries, err := provider.Client().ImageList(c.ctx, image.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("listing images failed: %w", err)
	}

	for _, s := range summaries {
		if s.ID != ci.ContainerJSONBase.Image {
			continue
		}
		var digest []string
		for _, d := range s.RepoDigests {
			if _, suffix, found := strings.Cut(d, "@"); found {
				digest = append(digest, suffix)
			} else {
				digest = append(digest, d)
			}
		}
		return fmt.Sprintf("%s (%s)", dc.Image, strings.Join(digest, ",")), nil
	}

	return "unknown", nil
}
