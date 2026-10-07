package system

import (
	"context"
	"log"
	"strings"
)

const documentationReviewPolicyVersion = 1

func documentationOnly(files []string) bool {
	hasMarkdown := false
	for _, file := range files {
		file = strings.ToLower(strings.ReplaceAll(file, "\\", "/"))
		if strings.HasSuffix(file, ".md") {
			hasMarkdown = true
			continue
		}
		if file == "deploy_commit.txt" || strings.HasPrefix(file, "commits/blog/") && strings.HasSuffix(file, ".html") {
			continue
		}
		return false
	}
	return hasMarkdown
}

func documentationReviewInstructions(files []string) string {
	if !documentationOnly(files) {
		return ""
	}
	return "\nDocumentation-only review: the trusted Git service confirms that this PR changes only Markdown and release notes. Review the document for the audience and scope explicitly requested by the task. Block only material factual errors, missing essential usage steps, unsafe instructions, or failure to satisfy that request. Do not block on wording preferences, optional edge cases, repeated caveats, internal implementation details unnecessary for the intended reader, or pre-existing software bugs that this documentation task does not change. Read the complete document: a caveat elsewhere may already resolve an apparent ambiguity; do not demand it in every step. For each blocking finding quote the exact incorrect instruction, show the conflicting behavior in the relevant current code, and explain the actual user consequence. Full-page reloads discard unsaved form edits even when the AJAX request did not submit them. Report all substantive issues together. If the guide is accurate and usable for its requested audience, return passed even if you would phrase it differently. Never change production code to satisfy a documentation-only task.\n"
}

// Reassess legacy documentation jobs stopped by the older general-purpose review
// criteria once. Do not grant approval or reset the fix budget.
func (c *Controller) recoverDocumentationReviews(ctx context.Context) {
	for _, j := range c.State.Jobs {
		if j.ReviewPolicyVersion >= documentationReviewPolicyVersion || j.Stage != "waiting" || j.Role != "executor" || j.PR == 0 || j.External || j.ClosedPR || !strings.HasPrefix(j.Question, "The automatic fix limit has been reached.") {
			continue
		}
		review, e := c.Runner.Broker(ctx, j, "review")
		if e != nil {
			log.Printf("documentation reassessment %s: %v", j.ID, e)
			continue
		}
		if !documentationOnly(review.Files) {
			continue
		}
		j.ReviewPolicyVersion = documentationReviewPolicyVersion
		j.Stage = "review"
		j.Role = "reviewer"
		j.ReviewerSession = ""
		j.Session = ""
		j.Question = ""
		j.QuestionMessage = 0
		j.Answer = "Reassess this documentation-only task independently under the documentation review criteria. Preserve the requested simple language and scope. Read the complete guide and verify earlier findings against the actual UI/code; do not repeat a resolved or unsupported finding. This reassessment does not approve changes or authorize changes to production code."
	}
}
