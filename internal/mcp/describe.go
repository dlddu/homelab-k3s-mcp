package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// renderPodDescribeText renders a kubectl-describe-style text snapshot.
func renderPodDescribeText(d *k8s.PodDescription) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Name:         %s\n", d.Name)
	fmt.Fprintf(&b, "Namespace:    %s\n", d.Namespace)
	if d.Node != nil {
		fmt.Fprintf(&b, "Node:         %s\n", *d.Node)
	}
	if d.StartTime != nil {
		fmt.Fprintf(&b, "Start Time:   %s\n", *d.StartTime)
	}
	if len(d.Labels) > 0 {
		b.WriteString("Labels:\n")
		for _, k := range sortedKeys(d.Labels) {
			fmt.Fprintf(&b, "  %s=%s\n", k, d.Labels[k])
		}
	}
	if len(d.Annotations) > 0 {
		b.WriteString("Annotations:\n")
		for _, k := range sortedKeys(d.Annotations) {
			fmt.Fprintf(&b, "  %s=%s\n", k, d.Annotations[k])
		}
	}
	phase := "<unknown>"
	if d.Phase != nil {
		phase = *d.Phase
	}
	fmt.Fprintf(&b, "Status:       %s\n", phase)
	if d.PodIP != nil {
		fmt.Fprintf(&b, "IP:           %s\n", *d.PodIP)
	}
	if d.HostIP != nil {
		fmt.Fprintf(&b, "Host IP:      %s\n", *d.HostIP)
	}
	if d.QOSClass != nil {
		fmt.Fprintf(&b, "QoS Class:    %s\n", *d.QOSClass)
	}
	if d.ServiceAccount != nil {
		fmt.Fprintf(&b, "Service Account: %s\n", *d.ServiceAccount)
	}

	if len(d.InitContainers) > 0 {
		b.WriteString("Init Containers:\n")
		for i := range d.InitContainers {
			writeContainer(&b, &d.InitContainers[i])
		}
	}
	if len(d.Containers) > 0 {
		b.WriteString("Containers:\n")
		for i := range d.Containers {
			writeContainer(&b, &d.Containers[i])
		}
	}

	if len(d.Conditions) > 0 {
		b.WriteString("Conditions:\n")
		b.WriteString("  Type              Status\n")
		for _, c := range d.Conditions {
			fmt.Fprintf(&b, "  %-17s %s\n", c.Type, c.Status)
		}
	}

	if len(d.Events) == 0 {
		b.WriteString("Events:       <none>\n")
	} else {
		b.WriteString("Events:\n")
		for _, e := range d.Events {
			when := "<unknown>"
			switch {
			case e.LastTimestamp != nil:
				when = *e.LastTimestamp
			case e.FirstTimestamp != nil:
				when = *e.FirstTimestamp
			}
			fmt.Fprintf(&b, "  %s %s (%dx): %s - %s\n", when, e.Type, e.Count, e.Reason, e.Message)
		}
	}

	return b.String()
}

func writeContainer(b *strings.Builder, c *k8s.ContainerInfo) {
	fmt.Fprintf(b, "  %s:\n", c.Name)
	fmt.Fprintf(b, "    Image:         %s\n", c.Image)
	if c.State != nil {
		fmt.Fprintf(b, "    State:         %s\n", *c.State)
	}
	if c.Reason != nil {
		fmt.Fprintf(b, "      Reason:      %s\n", *c.Reason)
	}
	if c.Message != nil {
		fmt.Fprintf(b, "      Message:     %s\n", *c.Message)
	}
	if c.ExitCode != nil {
		fmt.Fprintf(b, "      Exit Code:   %d\n", *c.ExitCode)
	}
	fmt.Fprintf(b, "    Ready:         %t\n", c.Ready)
	fmt.Fprintf(b, "    Restart Count: %d\n", c.RestartCount)
	if c.LastState != nil {
		fmt.Fprintf(b, "    Last State:    %s\n", *c.LastState)
		if c.LastReason != nil {
			fmt.Fprintf(b, "      Reason:      %s\n", *c.LastReason)
		}
		if c.LastExitCode != nil {
			fmt.Fprintf(b, "      Exit Code:   %d\n", *c.LastExitCode)
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
