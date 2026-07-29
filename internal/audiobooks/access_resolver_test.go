package audiobooks

import (
	"context"
	"testing"

	"github.com/Silo-Server/silo-server/internal/access"
)

type recordingScopeResolver struct {
	input access.ResolveInput
}

func (r *recordingScopeResolver) Resolve(_ context.Context, input access.ResolveInput) (access.Scope, error) {
	r.input = input
	return access.Scope{UserID: input.UserID, ProfileID: input.ProfileID}, nil
}

func TestABSAccessResolverPreservesSelectedProfileScope(t *testing.T) {
	resolver := &recordingScopeResolver{}
	absResolver := NewABSAccessResolver(nil, nil, resolver)

	filter, err := absResolver.ResolveABSAccess(context.Background(), "42", "primary-profile")
	if err != nil {
		t.Fatalf("ResolveABSAccess: %v", err)
	}
	if resolver.input.ProfileID != "primary-profile" {
		t.Fatalf("resolved profile = %q, want selected profile", resolver.input.ProfileID)
	}
	if !resolver.input.SkipPINVerification {
		t.Fatal("ABS-authenticated request did not skip duplicate PIN verification")
	}
	if filter.ProfileID != "primary-profile" {
		t.Fatalf("access filter profile = %q, want selected profile", filter.ProfileID)
	}
}
