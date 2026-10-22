package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// noResourcePermissionKind names what a ⑵ marker still lets a tool reach.
type noResourcePermissionKind string

const (
	// touchesNothing marks a tool that never speaks to the apiserver.
	touchesNothing noResourcePermissionKind = "no kubernetes access"

	// discoveryOnly marks a tool that only reads which kinds are served.
	discoveryOnly noResourcePermissionKind = "discovery only"
)

// noResourcePermission is AC1's ⑵ marker: its kind and the reason AC1 asks for.
type noResourcePermission struct {
	kind   noResourcePermissionKind
	reason string
}

type externalWrite struct {
	action string
	reason string

	precheck func(*Handler, context.Context, json.RawMessage) (any, *rpcErr)
}

// validateDeclarations enforces the startup half of prd-approval-gate AC1.
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
		problems = append(problems, validateExternalWrite(name, d)...)
	}
	sort.Strings(problems)
	return problems
}

func validateExternalWrite(name string, d toolDeclaration) []string {
	w := d.externalWrite
	if w == nil {
		return nil
	}
	switch {
	case d.noResourcePermission == nil || d.noResourcePermission.kind != touchesNothing:
		return []string{fmt.Sprintf(
			"%s declares an external write gate but is not marked as touching no kubernetes resource"+
				" — a tool with pairs is gated by its verbs (prd-approval-gate AC1)", name)}
	case w.action == "" || w.reason == "":
		return []string{fmt.Sprintf(
			"%s declares an external write gate without its action and the document that requires it", name)}
	case w.precheck == nil:
		return []string{fmt.Sprintf("%s declares an external write gate without a precheck", name)}
	}
	return nil
}
