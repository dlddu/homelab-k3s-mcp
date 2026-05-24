// Package version holds the server's build identity.
package version

const (
	// Name is the server/service name advertised over MCP and on the root route.
	Name = "homelab-k3s-mcp"
	// Version is the server version reported by the health endpoints and MCP initialize.
	Version = "0.1.0"
)
