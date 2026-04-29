// Package planio reads/writes the §9 plan + answer JSON contracts to a repo's
// .prismconductor/ directory.
package planio

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"prismconductor/internal/types"
)

const (
	subdirPlans   = ".prismconductor/plans"
	subdirAnswers = ".prismconductor/answers"
)

// PlanPath returns the canonical path for a given (issue, revision) plan file.
func PlanPath(repoPath string, issueNumber, revision int) string {
	return filepath.Join(repoPath, subdirPlans, fmt.Sprintf("%d-rev%d.json", issueNumber, revision))
}

// AnswerPath returns the canonical path for the answers file the user submits.
func AnswerPath(repoPath string, issueNumber, revision int) string {
	return filepath.Join(repoPath, subdirAnswers, fmt.Sprintf("%d-rev%d.json", issueNumber, revision))
}

// ReadPlan reads + validates a plan JSON file against the §9.1 contract.
func ReadPlan(path string) (*types.Plan, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p types.Plan
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("invalid plan JSON at %s: %w", path, err)
	}
	if p.IssueNumber == 0 || p.WorkspaceID == "" || p.Revision == 0 {
		return nil, errors.New("plan JSON missing required fields (issue_number, workspace_id, revision)")
	}
	if p.GeneratedAt.IsZero() {
		p.GeneratedAt = time.Now()
	}
	return &p, nil
}

// WriteAnswers persists answers in the schema the worker expects.
type AnswerSet struct {
	IssueNumber int                 `json:"issue_number"`
	Revision    int                 `json:"revision"`
	Answers     map[string]string   `json:"answers"`
	Multi       map[string][]string `json:"multi,omitempty"`
}

func WriteAnswers(repoPath string, issueNumber, revision int, set AnswerSet) error {
	path := AnswerPath(repoPath, issueNumber, revision)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	set.IssueNumber = issueNumber
	set.Revision = revision
	b, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ParsePlanPath extracts issue_number and revision from a plan filename like
// "1130-rev3.json" or a path containing one. Returns (0,0,err) on no match.
var planFilenameRE = regexp.MustCompile(`(\d+)-rev(\d+)\.json$`)

func ParsePlanPath(path string) (issueNumber, revision int, err error) {
	m := planFilenameRE.FindStringSubmatch(filepath.Base(path))
	if m == nil {
		return 0, 0, fmt.Errorf("not a plan filename: %s", path)
	}
	n, _ := strconv.Atoi(m[1])
	r, _ := strconv.Atoi(m[2])
	return n, r, nil
}

// LatestRevision scans the plans dir and returns the highest revision number
// for the given issue, or 0 if none.
func LatestRevision(repoPath string, issueNumber int) (int, error) {
	dir := filepath.Join(repoPath, subdirPlans)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var revs []int
	for _, e := range entries {
		n, r, err := ParsePlanPath(e.Name())
		if err != nil || n != issueNumber {
			continue
		}
		revs = append(revs, r)
	}
	sort.Ints(revs)
	if len(revs) == 0 {
		return 0, nil
	}
	return revs[len(revs)-1], nil
}
