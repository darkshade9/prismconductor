package skills

import (
	"io/fs"
	"strings"

	"prismconductor/internal/types"
)

// ScanForPipeline returns skills available for use as pipeline steps (issue #146).
// It wraps Discover() and enriches each SkillRef with a description extracted
// from the YAML frontmatter of the skill's markdown body. Skills without a
// description field are returned with an empty Description.
func ScanForPipeline(repoPath string, bundleFS fs.FS) ([]types.SkillRef, error) {
	refs, err := Discover(repoPath, bundleFS)
	if err != nil {
		return nil, err
	}
	for i := range refs {
		if refs[i].Source == "bundled" {
			name := strings.TrimPrefix(refs[i].Path, "bundled:")
			if body, readErr := fs.ReadFile(bundleFS, "skills/"+name+".md"); readErr == nil {
				refs[i].Description = extractFrontmatterDescription(string(body))
			}
		}
	}
	return refs, nil
}

// extractFrontmatterDescription parses the "description:" field from a YAML
// frontmatter block (--- ... ---) at the top of a markdown file. Returns ""
// if no frontmatter or description field is found.
func extractFrontmatterDescription(body string) string {
	if !strings.HasPrefix(body, "---") {
		return ""
	}
	rest := body[3:]
	end := strings.Index(rest, "---")
	if end < 0 {
		return ""
	}
	block := rest[:end]
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			return strings.Trim(val, "\"'")
		}
	}
	return ""
}
