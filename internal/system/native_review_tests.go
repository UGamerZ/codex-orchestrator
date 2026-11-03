package system

import "context"

// Remove legacy test copies only; new reviews run directly in the task workspace.
func (r *Runner) cleanupDockerReviewTests(ctx context.Context, id string) error {
	_, err := r.docker(ctx, nil, "run", "--rm", "--label", "codex-orchestrator="+r.namespace(), "--network", "none", "--read-only", "--user", "1000:1000", "--cap-drop=ALL", "--security-opt=no-new-privileges:true", "--pids-limit=32", "--memory=128m", "--cpus=1", "--mount", "type=volume,src="+r.volume(id, "reviewer-home")+",dst=/home/codex", r.C.Image, "cleanup-review-tests")
	return err
}
