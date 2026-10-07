package broker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Asset struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Data   []byte `json:"data,omitempty"`
	Error  string `json:"error,omitempty"`
}

func prepareContext(r Request, p Paths, git func(string, ...string) ([]byte, error), repo string) error {
	if len(r.Assets) > 32 {
		return fmt.Errorf("task has more than 32 linked files")
	}
	tmp, e := os.MkdirTemp(p.Review, ".context-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	if e = os.Chmod(p.Review, 0755); e != nil {
		return e
	}
	if e = os.Chmod(tmp, 0755); e != nil {
		return e
	}
	if e = os.Mkdir(filepath.Join(tmp, "attachments"), 0755); e != nil {
		return e
	}
	total := 0
	manifest := make([]Asset, 0, len(r.Assets))
	for _, a := range r.Assets {
		if a.Name == "" || a.Name != filepath.Base(a.Name) || strings.ContainsAny(a.Name, "/\\:\x00") || a.Name == "." || a.Name == ".." {
			return fmt.Errorf("invalid task asset name")
		}
		total += len(a.Data)
		if len(a.Data) > 16<<20 || total > 64<<20 {
			return fmt.Errorf("task attachments exceed size limit")
		}
		if a.Error == "" {
			if e = os.WriteFile(filepath.Join(tmp, "attachments", a.Name), a.Data, 0644); e != nil {
				return e
			}
		}
		a.Data = nil
		manifest = append(manifest, a)
	}
	b, _ := json.MarshalIndent(manifest, "", "  ")
	if e = os.WriteFile(filepath.Join(tmp, "attachments.json"), b, 0644); e != nil {
		return e
	}
	// This runs in the trusted publisher and only fetches the selected project's branches.
	if _, e = git(repo, "fetch", "--prune", "--no-tags", "origin", "+refs/heads/*:refs/remotes/origin/*"); e != nil {
		return e
	}
	bundle := filepath.Join(p.Publish, "references.bundle")
	if _, e = git(repo, "bundle", "create", bundle, "--remotes=origin"); e != nil {
		return e
	}
	info, e := os.Stat(bundle)
	if e != nil {
		return e
	}
	if info.Size() > 512<<20 {
		return fmt.Errorf("read-only branch bundle exceeds 512 MiB")
	}
	refs := filepath.Join(tmp, "branches.git")
	if _, e = git(tmp, "init", "--bare", refs); e != nil {
		return e
	}
	if _, e = git(refs, "-c", "protocol.file.allow=always", "fetch", "--no-tags", bundle, "+refs/remotes/origin/*:refs/heads/*"); e != nil {
		return e
	}
	// No remote, host paths, hooks or authentication are exported to agents.
	if e = os.WriteFile(filepath.Join(refs, "config"), []byte("[core]\n\tbare = true\n[gc]\n\tauto = 0\n"), 0644); e != nil {
		return e
	}
	os.Remove(filepath.Join(refs, "FETCH_HEAD"))
	branches, e := git(refs, "for-each-ref", "--format=%(refname:short) %(objectname)", "refs/heads/")
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(tmp, "branches.txt"), branches, 0644); e != nil {
		return e
	}
	target := filepath.Join(p.Review, "context")
	if e = os.RemoveAll(target); e != nil {
		return e
	}
	return os.Rename(tmp, target)
}
