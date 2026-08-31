package plan

import (
	"strings"
	"testing"
)

// ParseRewrite is reached today only through internal/cli's end-to-end rewrite
// tests, which drive it with well-formed plans. Every rejection branch was
// therefore unexecuted -- gremlins reported six NOT COVERED mutants across this
// file, meaning a mutation to any guard would have gone unnoticed.

// TestParseRewrite_Rejections covers each structural guard, one case per
// branch, asserting on the message so a guard cannot be silently weakened.
func TestParseRewrite_Rejections(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{
			name: "malformed JSON",
			data: `{"rewrites": [`,
			want: "rewrite plan parse error",
		},
		{
			name: "no rewrites key",
			data: `{}`,
			want: "rewrite plan has no rewrites",
		},
		{
			name: "empty rewrites array",
			data: `{"rewrites": []}`,
			want: "rewrite plan has no rewrites",
		},
		{
			name: "rewrite without a commit SHA",
			data: `{"rewrites":[{"commits":[{"message":"m","files":[{"path":"a.go"}]}]}]}`,
			want: "rewrite 0 has no commit",
		},
		{
			name: "the same commit rewritten twice",
			data: `{"rewrites":[
				{"commit":"abc123","commits":[{"message":"m","files":[{"path":"a.go"}]}]},
				{"commit":"abc123","commits":[{"message":"n","files":[{"path":"b.go"}]}]}]}`,
			want: "commit abc123 appears in more than one rewrite",
		},
		{
			name: "rewrite with no replacements",
			data: `{"rewrites":[{"commit":"abc123","commits":[]}]}`,
			want: "rewrite of abc123 has no replacement commits",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := ParseRewrite([]byte(c.data))
			if err == nil {
				t.Fatalf("expected rejection, got plan %+v", p)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), c.want)
			}
			assertHasHint(t, err)
		})
	}
}
