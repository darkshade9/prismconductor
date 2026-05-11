package skills_test

import (
	"os"
	"path/filepath"
	"testing"

	"prismconductor/internal/skills"
	"prismconductor/internal/skills/bundle"
	"prismconductor/internal/types"
)

func TestDiscover_BundledAlwaysPresent(t *testing.T) {
	refs, err := skills.Discover("", bundle.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) == 0 {
		t.Fatal("expected bundled skills, got none")
	}
	for _, r := range refs {
		if r.Source != "bundled" {
			t.Errorf("ref %q: expected source=bundled, got %q", r.Name, r.Source)
		}
		want := "bundled:" + r.Name
		if r.Path != want {
			t.Errorf("ref %q: expected path=%q, got %q", r.Name, want, r.Path)
		}
	}
}

func TestDiscover_RepoSkillAdded(t *testing.T) {
	dir := t.TempDir()
	cmdDir := filepath.Join(dir, ".claude", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "my-skill.md"), []byte("# My Skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	refs, err := skills.Discover(dir, bundle.FS)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, r := range refs {
		if r.Name == "my-skill" {
			found = true
			if r.Source != "repo" {
				t.Errorf("expected source=repo, got %q", r.Source)
			}
		}
	}
	if !found {
		t.Error("my-skill not found in discovered skills")
	}
}

func TestDiscover_RepoOverridesShadowsBundled(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".prismconductor", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "conductor-plan.md"), []byte("# Override"), 0o644); err != nil {
		t.Fatal(err)
	}

	refs, err := skills.Discover(dir, bundle.FS)
	if err != nil {
		t.Fatal(err)
	}

	var count int
	for _, r := range refs {
		if r.Name == "conductor-plan" {
			count++
			if r.Source != "repo" {
				t.Errorf("expected repo override to win, got source=%q", r.Source)
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 conductor-plan entry, got %d", count)
	}
}

func TestDiscover_SubdirSKILLmd(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".claude", "skills", "start-issue")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Start Issue"), 0o644); err != nil {
		t.Fatal(err)
	}

	refs, err := skills.Discover(dir, bundle.FS)
	if err != nil {
		t.Fatal(err)
	}

	var found *types.SkillRef
	for i := range refs {
		if refs[i].Name == "start-issue" {
			found = &refs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("start-issue not found in discovered skills")
	}
	if found.Source != "repo" {
		t.Errorf("expected source=repo, got %q", found.Source)
	}
	if found.DisplayName != "start-issue (repo)" {
		t.Errorf("expected DisplayName=%q, got %q", "start-issue (repo)", found.DisplayName)
	}
}

func TestDiscover_SubdirSkillmdLowercase(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".claude", "skills", "my-tool")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# My Tool"), 0o644); err != nil {
		t.Fatal(err)
	}

	refs, err := skills.Discover(dir, bundle.FS)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, r := range refs {
		if r.Name == "my-tool" && r.Source == "repo" {
			found = true
		}
	}
	if !found {
		t.Error("my-tool not found via lowercase skill.md subdir pattern")
	}
}

func TestDiscover_SubdirDoesNotDuplicateFlatFile(t *testing.T) {
	dir := t.TempDir()
	// flat file
	flatDir := filepath.Join(dir, ".claude", "skills")
	if err := os.MkdirAll(flatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flatDir, "foo.md"), []byte("# Foo flat"), 0o644); err != nil {
		t.Fatal(err)
	}
	// subdir with same skill name
	subDir := filepath.Join(flatDir, "foo")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "SKILL.md"), []byte("# Foo subdir"), 0o644); err != nil {
		t.Fatal(err)
	}

	refs, err := skills.Discover(dir, bundle.FS)
	if err != nil {
		t.Fatal(err)
	}

	var count int
	for _, r := range refs {
		if r.Name == "foo" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 foo entry (flat file wins), got %d", count)
	}
}

func TestDiscover_NoDuplicatesFromMultipleGlobs(t *testing.T) {
	dir := t.TempDir()
	// Same file reachable via two different glob paths is impossible in practice
	// (different directories), but verify that two skills with the same name
	// from different directories results in the first-found winning.
	for _, sub := range []string{".prismconductor/skills", ".claude/commands"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, sub, "shared.md"), []byte("# "+sub), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	refs, err := skills.Discover(dir, bundle.FS)
	if err != nil {
		t.Fatal(err)
	}

	var count int
	for _, r := range refs {
		if r.Name == "shared" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 shared entry (first-found wins), got %d", count)
	}
}
