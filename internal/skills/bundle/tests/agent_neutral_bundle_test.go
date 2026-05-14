package tests

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	"prismconductor/internal/skills/bundle"
)

func TestBundledSkillsHaveCodexCompatibleFrontmatter(t *testing.T) {
	entries, err := fs.ReadDir(bundle.FS, "skills")
	if err != nil {
		t.Fatalf("read bundled skills: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := entry.Name()
		body, err := bundle.FS.ReadFile(path.Join("skills", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(body)
		if !strings.HasPrefix(text, "---\n") {
			t.Errorf("%s missing YAML frontmatter delimiter", name)
			continue
		}
		end := strings.Index(text[len("---\n"):], "\n---")
		if end < 0 {
			t.Errorf("%s missing closing YAML frontmatter delimiter", name)
			continue
		}
		frontmatter := text[len("---\n") : len("---\n")+end]
		if !hasFrontmatterField(frontmatter, "name") {
			t.Errorf("%s missing frontmatter name", name)
		}
		if !hasFrontmatterField(frontmatter, "description") {
			t.Errorf("%s missing frontmatter description", name)
		}
	}
}

func TestBundledSkillsDoNotDuplicateProviderVariants(t *testing.T) {
	entries, err := fs.ReadDir(bundle.FS, "skills")
	if err != nil {
		t.Fatalf("read bundled skills: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		for _, disallowed := range []string{"-codex", "-claude", "-openai", "-gemini", "-ollama", "-lmstudio", "-litellm"} {
			if strings.HasSuffix(name, disallowed) {
				t.Errorf("bundled skill %q is provider-specific; use one canonical skill plus provider invocation adapters", name)
			}
		}
	}
}

func hasFrontmatterField(frontmatter, field string) bool {
	prefix := field + ":"
	for _, line := range strings.Split(frontmatter, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true
		}
	}
	return false
}
