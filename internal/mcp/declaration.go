package mcp

import (
	"fmt"
	"sort"
)

type noResourcePermissionKind string

const (
	touchesNothing noResourcePermissionKind = "no kubernetes access"

	discoveryOnly noResourcePermissionKind = "discovery only"
)

type noResourcePermission struct {
	kind   noResourcePermissionKind
	reason string
}

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
