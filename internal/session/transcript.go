package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// IssuePayloadPath returns the canonical path where the conductor pre-fetches
// the issue body before spawning a plan worker (issue #197).
func IssuePayloadPath(repoPath string, issueNumber int) string {
	return filepath.Join(repoPath, ".prismconductor", fmt.Sprintf("issue-%d.md", issueNumber))
}

// ScanForIssueRead scans a stream-json transcript file and returns true when
// there is evidence the planner actually read the GitHub issue. Accepted
// evidence (any one is sufficient):
//   - A Bash tool call whose command contains "gh issue view <issueNumber>"
//   - A Read tool call whose file_path references the pre-fetched issue payload
//   - A WebFetch tool call whose URL path contains "/issues/<issueNumber>"
//
// The scan is forward-only and returns on the first hit. An I/O error is
// returned only for unexpected failures; a missing transcript is treated as
// no-evidence (false, nil) so the caller decides whether to block or warn.
func ScanForIssueRead(transcriptPath string, issueNumber int) (bool, error) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	numStr := strconv.Itoa(issueNumber)
	ghViewNeedle := "gh issue view " + numStr
	// Accept any path that ends with "issue-<N>.md" or contains ".prismconductor/issue-<N>"
	issueFileSuffix := "issue-" + numStr + ".md"
	issueFilePrisma := ".prismconductor/issue-" + numStr
	webNeedle := "/issues/" + numStr

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "{") {
			continue
		}
		found, err := scanLineForIssueRead(line, ghViewNeedle, issueFileSuffix, issueFilePrisma, webNeedle)
		if err != nil || found {
			return found, err
		}
	}
	return false, scanner.Err()
}

// scanLineForIssueRead checks a single stream-json line for issue-read evidence.
// Extracted so it can be unit-tested without a real file.
func scanLineForIssueRead(line, ghViewNeedle, issueFileSuffix, issueFilePrisma, webNeedle string) (bool, error) {
	var msg struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type  string          `json:"type"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil || msg.Type != "assistant" {
		return false, nil
	}
	for _, block := range msg.Message.Content {
		if block.Type != "tool_use" {
			continue
		}
		switch block.Name {
		case "Bash":
			var in struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(block.Input, &in) == nil {
				if strings.Contains(in.Command, ghViewNeedle) {
					return true, nil
				}
			}
		case "Read":
			var in struct {
				FilePath string `json:"file_path"`
			}
			if json.Unmarshal(block.Input, &in) == nil {
				fp := filepath.ToSlash(in.FilePath)
				if strings.HasSuffix(fp, issueFileSuffix) || strings.Contains(fp, issueFilePrisma) {
					return true, nil
				}
			}
		case "WebFetch":
			var in struct {
				URL string `json:"url"`
			}
			if json.Unmarshal(block.Input, &in) == nil {
				if strings.Contains(in.URL, webNeedle) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}
