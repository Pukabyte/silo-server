package audiobooks

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Silo-Server/silo-server/internal/access"
	"github.com/Silo-Server/silo-server/internal/catalog"
	"github.com/Silo-Server/silo-server/internal/userstore"
)

// ABSAccessResolver adapts silo's native profile/library access resolver to
// the ABS compatibility layer. ABS has already authenticated the account with
// a password or refresh token, so profile PIN verification is skipped here.
type ABSAccessResolver struct {
	resolver scopeResolver
	stores   userstore.UserStoreProvider
}

type scopeResolver interface {
	Resolve(ctx context.Context, input access.ResolveInput) (access.Scope, error)
}

// NewABSAccessResolver creates a resolver for ABS-authenticated access checks.
func NewABSAccessResolver(
	users access.UserRepository,
	stores userstore.UserStoreProvider,
	resolver scopeResolver,
	groups ...access.GroupPolicyProvider,
) *ABSAccessResolver {
	if resolver != nil {
		return &ABSAccessResolver{resolver: resolver, stores: stores}
	}
	if users == nil || stores == nil {
		return nil
	}
	// Legacy resolver: proxy/test wiring without a policy system. Production integrated/api modes always take the policy path. Removed with the legacy cleanup phase.
	return &ABSAccessResolver{resolver: access.NewResolver(users, stores, nil, groups...), stores: stores}
}

func (r *ABSAccessResolver) ResolveABSAccess(ctx context.Context, userID, profileID string) (catalog.AccessFilter, error) {
	if r == nil || r.resolver == nil {
		return catalog.AccessFilter{}, nil
	}
	uid, err := strconv.Atoi(userID)
	if err != nil {
		return catalog.AccessFilter{}, fmt.Errorf("invalid ABS user id %q: %w", userID, err)
	}
	// Audiobookshelf has account users, not Silo household profiles. A normal
	// ABS login selects Silo's primary profile, which represents that account
	// rather than an additional access boundary. Resolve it at the account
	// scope, matching upstream's req.user.checkCanAccessLibrary behavior.
	// Explicit non-primary profile logins (username#profile) remain scoped to
	// the selected profile and keep their Silo restrictions.
	effectiveProfileID := profileID
	if profileID != "" && r.stores != nil {
		store, storeErr := r.stores.ForUser(ctx, uid)
		if storeErr != nil {
			return catalog.AccessFilter{}, fmt.Errorf("open ABS profile store for %d: %w", uid, storeErr)
		}
		profile, profileErr := store.GetProfile(ctx, profileID)
		if profileErr != nil {
			return catalog.AccessFilter{}, fmt.Errorf("load ABS profile %s: %w", profileID, profileErr)
		}
		if profile != nil && profile.IsPrimary {
			effectiveProfileID = ""
		}
	}
	scope, err := r.resolver.Resolve(ctx, access.ResolveInput{
		UserID:              uid,
		ProfileID:           effectiveProfileID,
		SkipPINVerification: true,
	})
	if err != nil {
		return catalog.AccessFilter{}, err
	}
	return catalog.AccessFilter{
		AllowedLibraryIDs:  scope.AllowedLibraryIDs,
		DisabledLibraryIDs: scope.DisabledLibraryIDs,
		MaxContentRating:   scope.MaxContentRating,
		MaxPlaybackQuality: scope.MaxPlaybackQuality,
		UserID:             scope.UserID,
		ProfileID:          scope.ProfileID,
	}, nil
}
