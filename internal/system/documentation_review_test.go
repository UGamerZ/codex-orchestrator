package system

import "testing"

func TestDocumentationReviewScope(t *testing.T) {
	for _, tc := range []struct {
		files []string
		want  bool
	}{
		{[]string{"PARSER_GUIDE.md"}, true},
		{[]string{"PARSER_GUIDE.md", "deploy_commit.txt", "commits/blog/update.html"}, true},
		{[]string{"PARSER_GUIDE.md", "project/api/Simpla.php"}, false},
		{[]string{"project/design/page.html"}, false},
		{nil, false},
	} {
		if got := documentationReviewInstructions(tc.files) != ""; got != tc.want {
			t.Fatalf("review scope for %v: %v", tc.files, got)
		}
	}
}
