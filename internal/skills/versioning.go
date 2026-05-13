package skills

import (
	"crypto/sha256"
	"fmt"
)

// HashSkillMarkdown returns the hex-encoded SHA-256 of the skill markdown
// content. Computed once at spawn time so the hash reflects the exact skill
// version that drove a session, not whatever is on disk at session end.
func HashSkillMarkdown(content []byte) string {
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%x", sum)
}
