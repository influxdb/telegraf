package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	incus "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

var (
	timeout     = 120
	killTimeout = 5
)

type BytesBuffer struct {
	*bytes.Buffer
}

func (b *BytesBuffer) Close() error {
	return nil
}

type IncusClient struct {
	Client incus.InstanceServer
}

// Connect to the LXD socket.
func (c *IncusClient) Connect() error {
	client, err := incus.ConnectIncusUnix("", nil)
	if err != nil {
		return err
	}

	c.Client = client
	return nil
}

// Create a container using a specific remote and alias.
func (c *IncusClient) Create(name, remote, alias string) error {
	fmt.Printf("creating %s with %s:%s\n", name, remote, alias)

	if c.Client == nil {
		err := c.Connect()
		if err != nil {
			return err
		}
	}

	server := ""
	switch remote {
	case "images":
		server = "https://images.linuxcontainers.org"
	case "ubuntu":
		server = "https://cloud-images.ubuntu.com/releases"
	case "ubuntu-daily":
		server = "https://cloud-images.ubuntu.com/daily"
	default:
		return fmt.Errorf("unknown remote: %s", remote)
	}

	req := api.InstancesPost{
		Name: name,
		Source: api.InstanceSource{
			Type:     "image",
			Mode:     "pull",
			Protocol: "simplestreams",
			Server:   server,
			Alias:    alias,
		},
	}

	// Get LXD to create the container (background operation)
	op, err := c.Client.CreateInstance(req)
	if err != nil {
		return err
	}

	// Wait for the operation to complete
	err = op.Wait()
	if err != nil {
		return err
	}

	return nil
}

// Delete the given container.
func (c *IncusClient) Delete(name string) error {
	fmt.Println("deleting", name)
	op, err := c.Client.DeleteInstance(name)
	if err != nil {
		return err
	}

	return op.Wait()
}

// Run a command returning result struct. Will kill commands that take longer
// than 120 seconds.
func (c *IncusClient) Exec(name string, command ...string) error {
	fmt.Printf("$ %s\n", strings.Join(command, " "))

	cmd := []string{"/usr/bin/timeout", "-k", strconv.Itoa(killTimeout), strconv.Itoa(timeout)}
	cmd = append(cmd, command...)
	req := api.InstanceExecPost{
		Command:   cmd,
		WaitForWS: true,
	}

	output := &BytesBuffer{bytes.NewBuffer(nil)}

	args := incus.InstanceExecArgs{
		Stdout:   output,
		Stderr:   output,
		DataDone: make(chan bool),
	}

	op, err := c.Client.ExecInstance(name, req, &args)
	if err != nil {
		return err
	}

	err = op.Wait()
	if err != nil {
		return err
	}

	// Wait for any remaining I/O to be flushed
	<-args.DataDone

	// get the return code
	opAPI := op.Get()
	rc := int(opAPI.Metadata["return"].(float64))

	if rc != 0 {
		return errors.New(output.String())
	}

	fmt.Println(output.String())

	return nil
}

// Push file to container.
func (c *IncusClient) Push(name, src, dst string) error {
	fmt.Printf("cp %s %s%s\n", src, name, dst)
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("error reading %s: %w", src, err)
	}
	defer f.Close()

	return c.Client.CreateInstanceFile(name, dst, incus.InstanceFileArgs{
		Content: f,
		Mode:    0644,
	})
}

// Start the given container.
func (c *IncusClient) Start(name string) error {
	fmt.Println("starting", name)
	reqState := api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}

	op, err := c.Client.UpdateInstanceState(name, reqState, "")
	if err != nil {
		return err
	}

	return op.Wait()
}

// Stop the given container.
func (c *IncusClient) Stop(name string) error {
	fmt.Println("stopping", name)
	reqState := api.InstanceStatePut{
		Action:  "stop",
		Force:   true,
		Timeout: 10,
	}

	op, err := c.Client.UpdateInstanceState(name, reqState, "")
	if err != nil {
		return err
	}

	return op.Wait()
}
