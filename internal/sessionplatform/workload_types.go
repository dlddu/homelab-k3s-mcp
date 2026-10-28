package sessionplatform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// configPath is the control plane's runtime configuration endpoint. It lists
// the workload types the deployment offers, so this server keeps no copy of
// that list.
const configPath = "/api/v1/config"

// shellWorkloadType is the control plane's default and is offered by every
// deployment, so naming it needs no lookup.
const shellWorkloadType = "shell"

// A control plane old enough to publish no list is left to judge the type
// itself; it refuses an unknown one with a 400, which CreateSession already
// reports as an invalid argument.
func (c *Client) checkWorkloadType(ctx context.Context, workloadType string) error {
	if workloadType == "" || workloadType == shellWorkloadType {
		return nil
	}
	offered, err := c.offeredWorkloadTypes(ctx)
	if err != nil {
		return err
	}
	if offered != nil && !slices.Contains(offered, workloadType) {
		return invalidArgument(fmt.Sprintf("workloadType must be one of %s, got %q",
			strings.Join(offered, ", "), workloadType))
	}
	return nil
}

func (c *Client) offeredWorkloadTypes(ctx context.Context) ([]string, error) {
	endpoint := c.endpoint + configPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, apiError(fmt.Sprintf("get %s: %v", endpoint, err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apiError(fmt.Sprintf("%s returned %s: %s", endpoint, resp.Status, string(body)))
	}

	var config struct {
		WorkloadTypes *[]struct {
			Type string `json:"type"`
		} `json:"workloadTypes"`
	}
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, apiError(fmt.Sprintf("parse %s: %v", endpoint, err))
	}
	if config.WorkloadTypes == nil {
		return nil, nil
	}
	offered := make([]string, 0, len(*config.WorkloadTypes))
	for _, t := range *config.WorkloadTypes {
		if t.Type != "" {
			offered = append(offered, t.Type)
		}
	}
	return offered, nil
}
