package mcp

import (
	"fmt"
	"sort"
)

// noResourcePermissionKind names what a ⑵ marker still lets a tool reach.
// AC1 makes the marker an explicit statement rather than an absence, so the
// statement has to say which of the two shapes it is: a tool that never speaks
// to the apiserver and one that only asks it which kinds exist are different
// permissions, and the call-time half of AC1 confines them differently.
type noResourcePermissionKind string

const (
	// touchesNothing is the marker of a tool whose work happens entirely
	// outside the cluster — the platform integrations reach GitHub,
	// OpenSearch, AWS, Grafana or the session platform and never the
	// apiserver.
	touchesNothing noResourcePermissionKind = "no kubernetes access"

	// discoveryOnly is the marker prd-approval-gate AC1 carves out by name:
	// discovery is not a resource permission, so a tool that reads the served
	// kinds and nothing else declares no pair yet is not unmarked.
	discoveryOnly noResourcePermissionKind = "discovery only"
)

// noResourcePermission is AC1's ⑵: the explicit, reasoned statement that a
// tool exercises no (verb, resource) permission. It is a struct rather than a
// bool because AC1 asks for the reason too — "빈 선언" must not be able to pass
// as a marker, and a value that carries a sentence cannot be arrived at by
// forgetting to write one.
type noResourcePermission struct {
	kind   noResourcePermissionKind
	reason string
}

// validateDeclarations enforces the startup half of prd-approval-gate AC1:
// every registered tool carries exactly one of ⑴ a pair declaration (fixed or
// derived) and ⑵ a no-resource-permission marker with its reason.
//
// The check exists because the two states it separates are indistinguishable
// in the data otherwise. An empty pairs slice is what a platform integration
// that touches nothing looks like, and it is also what a tool whose author
// forgot the declaration looks like — and the second one reaches the cluster
// ungated, because the gate has no pair to judge. Requiring the marker makes
// forgetting a compile-time-shaped omission that startup can see.
func validateDeclarations(registered map[string]toolEntry) []string {
	var problems []string
	for name, entry := range registered {
		d := entry.decl
		declaresPairs := len(d.pairs) > 0 || d.resolve != nil
		marker := d.noResourcePermission

		switch {
		case !declaresPairs && marker == nil:
			problems = append(problems, fmt.Sprintf(
				"%s declares no (verb, resource) pair and carries no no-resource-permission marker"+
					" — an empty declaration is not a marker (prd-approval-gate AC1)", name))
		case declaresPairs && marker != nil:
			problems = append(problems, fmt.Sprintf(
				"%s both declares pairs and claims no resource permission — AC1 asks for exactly one", name))
		case marker != nil:
			switch marker.kind {
			case touchesNothing, discoveryOnly:
			default:
				problems = append(problems, fmt.Sprintf(
					"%s has a no-resource-permission marker of unknown kind %q", name, marker.kind))
			}
			if marker.reason == "" {
				problems = append(problems, fmt.Sprintf(
					"%s claims no resource permission without saying why (prd-approval-gate AC1)", name))
			}
		}
	}
	sort.Strings(problems)
	return problems
}
