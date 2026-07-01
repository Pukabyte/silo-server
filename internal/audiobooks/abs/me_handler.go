package abs

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleMe — GET /abs/api/me (and /api/me)
//
// Real ABS returns req.user.toOldJSONForBrowser() — the FULL user object, the
// same shape carried on the login/authorize envelope's `user`. A strict client
// decodes /me with its User model, so a thin {id,username,...} map crashes it
// on the first missing required key. Emit the shared absUserObject (minus the
// login-only accessToken/refreshToken; /me still carries the `token` slot set
// to the caller's presented bearer).
func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	a, ok := absAuthFrom(r)
	if !ok || a.UserID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	defaultLibID := VirtualLibraryID
	access, err := h.accessFilterForAuth(r.Context(), a)
	if err != nil {
		http.Error(w, "resolve access: "+err.Error(), http.StatusForbidden)
		return
	}
	libs, err := h.deps.MediaStore.ListAudiobookLibraries(r.Context(), access)
	if err == nil && len(libs) > 0 {
		defaultLibID = audiobookLibraryID(libs[0])
	}

	writeJSON(w, http.StatusOK, absUserObject(a.UserID, a.UserID, a.Token, defaultLibID, time.Now()))
}

// audiobookLibraryID returns the ABS-wire library ID string for an
// AudiobookLibrary. Numeric IDs are formatted as decimal strings; when
// the ID is 0 we fall back to the virtual constant.
func audiobookLibraryID(lib AudiobookLibrary) string {
	if lib.ID > 0 {
		return strconv.FormatInt(lib.ID, 10)
	}
	return VirtualLibraryID
}

// audiobookLibraryMap builds the minimal ABS library object shared by
// /libraries (list) and /libraries/{id} (detail). Plugin parity:
// {id, name, mediaType} only — the extra folders/displayOrder/icon
// fields some servers emit are ignored by real ABS clients and adding
// them risks behaviour drift.
func audiobookLibraryMap(lib AudiobookLibrary) map[string]any {
	name := strings.TrimSpace(lib.Name)
	if name == "" {
		name = VirtualLibraryName
	}
	return map[string]any{
		"id":        audiobookLibraryID(lib),
		"name":      name,
		"mediaType": LibraryMediaType,
	}
}
