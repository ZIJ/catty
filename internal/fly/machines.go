package fly

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// MachineConfig represents the configuration for creating a machine.
type MachineConfig struct {
	Image    string            `json:"image"`
	Env      map[string]string `json:"env,omitempty"`
	Services []MachineService  `json:"services,omitempty"`
	Guest    *GuestConfig      `json:"guest,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MachineService represents a service exposed by the machine.
type MachineService struct {
	Protocol     string        `json:"protocol"`
	InternalPort int           `json:"internal_port"`
	Ports        []ServicePort `json:"ports,omitempty"`
}

// ServicePort represents a port configuration for a service.
type ServicePort struct {
	Port     int      `json:"port"`
	Handlers []string `json:"handlers,omitempty"`
}

// GuestConfig represents machine resource configuration.
type GuestConfig struct {
	CPUs     int    `json:"cpus,omitempty"`
	MemoryMB int    `json:"memory_mb,omitempty"`
	CPUKind  string `json:"cpu_kind,omitempty"`
}

// CreateMachineRequest is the request body for creating a machine.
type CreateMachineRequest struct {
	Name   string         `json:"name,omitempty"`
	Region string         `json:"region,omitempty"`
	Config *MachineConfig `json:"config"`
}

// Machine represents a Fly machine.
type Machine struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	State        string         `json:"state"`
	Region       string         `json:"region"`
	InstanceID   string         `json:"instance_id"`
	PrivateIP    string         `json:"private_ip"`
	Config       *MachineConfig `json:"config"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	ProcessGroup string         `json:"process_group"`
}

// CreateMachine creates a new machine in the app.
func (c *Client) CreateMachine(req *CreateMachineRequest) (*Machine, error) {
	path := fmt.Sprintf("/v1/apps/%s/machines", c.appName)

	resp, err := c.do(http.MethodPost, path, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, readError(resp)
	}

	var machine Machine
	if err := json.NewDecoder(resp.Body).Decode(&machine); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &machine, nil
}

// GetMachine retrieves a machine by ID.
func (c *Client) GetMachine(machineID string) (*Machine, error) {
	path := fmt.Sprintf("/v1/apps/%s/machines/%s", c.appName, machineID)

	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readError(resp)
	}

	var machine Machine
	if err := json.NewDecoder(resp.Body).Decode(&machine); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &machine, nil
}

// WaitMachine waits for a machine to reach a specific state.
func (c *Client) WaitMachine(machineID, state string, timeout time.Duration) error {
	path := fmt.Sprintf("/v1/apps/%s/machines/%s/wait?state=%s&timeout=%d",
		c.appName, machineID, state, int(timeout.Seconds()))

	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return readError(resp)
	}

	// Drain the response body
	io.Copy(io.Discard, resp.Body)
	return nil
}

// StopMachine stops a running machine.
func (c *Client) StopMachine(machineID string) error {
	path := fmt.Sprintf("/v1/apps/%s/machines/%s/stop", c.appName, machineID)

	resp, err := c.do(http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return readError(resp)
	}

	io.Copy(io.Discard, resp.Body)
	return nil
}

// DeleteMachine deletes a machine.
func (c *Client) DeleteMachine(machineID string, force bool) error {
	path := fmt.Sprintf("/v1/apps/%s/machines/%s", c.appName, machineID)
	if force {
		path += "?force=true"
	}

	resp, err := c.do(http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return readError(resp)
	}

	io.Copy(io.Discard, resp.Body)
	return nil
}

// ListMachines lists machines in the app, optionally filtered by metadata.
func (c *Client) ListMachines(metadata map[string]string) ([]*Machine, error) {
	path := fmt.Sprintf("/v1/apps/%s/machines", c.appName)

	if len(metadata) > 0 {
		params := url.Values{}
		for k, v := range metadata {
			params.Add("metadata."+k, v)
		}
		path += "?" + params.Encode()
	}

	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readError(resp)
	}

	var machines []*Machine
	if err := json.NewDecoder(resp.Body).Decode(&machines); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return machines, nil
}

// GetCurrentImage returns the image reference from an existing machine in the app.
// Prefers machines from the "app" process group (created by fly deploy).
func (c *Client) GetCurrentImage() (string, error) {
	machines, err := c.ListMachines(nil)
	if err != nil {
		return "", err
	}

	// First, try to find a machine from "app" process group (fly deploy machines)
	// The process group is stored in metadata.fly_process_group
	for _, m := range machines {
		if m.Config != nil && m.Config.Metadata["fly_process_group"] == "app" && m.Config.Image != "" {
			return m.Config.Image, nil
		}
	}

	// Fallback to any machine with an image
	for _, m := range machines {
		if m.Config != nil && m.Config.Image != "" {
			return m.Config.Image, nil
		}
	}

	return "", fmt.Errorf("no machines found with image config")
}
