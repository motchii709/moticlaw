package tools

import (
	"fmt"
	"regexp"
)

var validUserID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// validateUserID validates that the userID contains only safe characters
// for use in file paths. Discord user IDs are numeric, but we don't rely
// on that assumption — we validate explicitly.
func validateUserID(userID string) error {
	if !validUserID.MatchString(userID) {
		return fmt.Errorf("invalid user ID: must be alphanumeric, underscore, or hyphen only")
	}
	return nil
}