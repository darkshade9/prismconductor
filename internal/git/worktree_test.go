package git

import (
	"reflect"
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Entry
	}{
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "single worktree",
			in: "worktree /repo\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main\n",
			want: []Entry{
				{Path: "/repo", HEAD: "abc123", Branch: "refs/heads/main"},
			},
		},
		{
			name: "two worktrees with trailing whitespace",
			in: "worktree /repo\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main\n" +
				"\n" +
				"worktree /repo/.prismconductor/worktrees/ws-7\n" +
				"HEAD def456\n" +
				"branch refs/heads/feat/issue-7-foo\n" +
				"\n",
			want: []Entry{
				{Path: "/repo", HEAD: "abc123", Branch: "refs/heads/main"},
				{Path: "/repo/.prismconductor/worktrees/ws-7", HEAD: "def456", Branch: "refs/heads/feat/issue-7-foo"},
			},
		},
		{
			name: "detached head has no branch line",
			in: "worktree /repo\n" +
				"HEAD abc123\n" +
				"detached\n",
			want: []Entry{
				{Path: "/repo", HEAD: "abc123"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseWorktreeList(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseWorktreeList\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}
