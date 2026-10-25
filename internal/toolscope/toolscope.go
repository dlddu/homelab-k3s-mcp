// Package toolscope carries the set of tools a request may see and call, from
// the auth layer that knows the caller to the dispatcher that knows the tools.
// Neither of those may import the other, so the scope lives here.
package toolscope

import "context"

type scopeKey struct{}

// With limits the rest of the request to tools.
func With(ctx context.Context, tools []string) context.Context {
	set := make(map[string]struct{}, len(tools))
	for _, t := range tools {
		set[t] = struct{}{}
	}
	return context.WithValue(ctx, scopeKey{}, set)
}

// Restricted reports whether With limited this request.
func Restricted(ctx context.Context) bool {
	_, ok := ctx.Value(scopeKey{}).(map[string]struct{})
	return ok
}

// Allows reports whether the request may see and call tool.
func Allows(ctx context.Context, tool string) bool {
	set, ok := ctx.Value(scopeKey{}).(map[string]struct{})
	if !ok {
		return true
	}
	_, in := set[tool]
	return in
}
