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

// TokenCap bounds the GitHub App installation tokens a scoped caller may mint.
type TokenCap struct {
	Repositories []string
	Permissions  map[string]string
}

type tokenCapKey struct{}

// WithTokenCap limits the installation tokens the rest of the request may mint.
func WithTokenCap(ctx context.Context, tokenCap *TokenCap) context.Context {
	if tokenCap == nil {
		return ctx
	}
	return context.WithValue(ctx, tokenCapKey{}, tokenCap)
}

// TokenCapFrom returns the cap WithTokenCap set, or nil when minting is unbounded.
func TokenCapFrom(ctx context.Context) *TokenCap {
	tokenCap, _ := ctx.Value(tokenCapKey{}).(*TokenCap)
	return tokenCap
}

type selfRepositoryKey struct{}

// WithSelfRepository records the repository whose workflow made the request.
func WithSelfRepository(ctx context.Context, name string) context.Context {
	if name == "" {
		return ctx
	}
	return context.WithValue(ctx, selfRepositoryKey{}, name)
}

// SelfRepository returns what WithSelfRepository recorded, or "".
func SelfRepository(ctx context.Context) string {
	name, _ := ctx.Value(selfRepositoryKey{}).(string)
	return name
}
