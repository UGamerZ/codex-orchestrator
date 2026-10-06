package broker

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func safeArchivePath(name string) bool {
	p := path.Clean(name)
	if p == "." || path.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") || strings.ContainsAny(name, "\\:\x00") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if strings.EqualFold(part, ".git") {
			return false
		}
	}
	return true
}

// Validate the entire archive before writing; archive files cannot be placed below
// a symlink, and link chains cannot escape the snapshot or refer to Git metadata.
func archiveLinks(data []byte) (map[string]string, error) {
	links := map[string]string{}
	names := []string{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if !safeArchivePath(h.Name) {
			return nil, fmt.Errorf("unsafe archive path: %s", h.Name)
		}
		name := path.Clean(h.Name)
		names = append(names, name)
		if h.Typeflag == tar.TypeSymlink {
			if _, exists := links[name]; exists {
				return nil, fmt.Errorf("duplicate archive link")
			}
			links[name] = h.Linkname
		}
	}
	for _, name := range names {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, ok := links[parent]; ok {
				return nil, fmt.Errorf("archive entry is below a symlink")
			}
		}
	}
	for name, target := range links {
		if _, e := resolveLink(name, target, links); e != nil {
			return nil, e
		}
	}
	return links, nil
}
func resolveLink(name, target string, links map[string]string) (string, error) {
	resolved, _, e := traceLink(name, target, links)
	return resolved, e
}
func traceLink(name, target string, links map[string]string) (string, []string, error) {
	active := map[string]bool{}
	dependencies := []string{}
	visits := 0
	var walk func(string, []string) ([]string, error)
	walk = func(value string, stack []string) ([]string, error) {
		if value == "" || path.IsAbs(value) || strings.ContainsAny(value, "\\:\x00") {
			return nil, fmt.Errorf("unsafe repository link: %s", name)
		}
		stack = append([]string{}, stack...)
		for _, part := range strings.Split(value, "/") {
			if part == "" || part == "." {
				continue
			}
			if part == ".." {
				if len(stack) == 0 {
					return nil, fmt.Errorf("repository link escapes working copy: %s", name)
				}
				stack = stack[:len(stack)-1]
				continue
			}
			if strings.EqualFold(part, ".git") {
				return nil, fmt.Errorf("repository link references Git metadata")
			}
			prefix := strings.Join(append(append([]string{}, stack...), part), "/")
			key := ""
			for candidate := range links {
				if strings.EqualFold(candidate, prefix) {
					key = candidate
					break
				}
			}
			if key != "" {
				visits++
				if visits > 64 || active[key] {
					return nil, fmt.Errorf("cyclic or excessive repository link chain")
				}
				active[key] = true
				dependencies = append(dependencies, key)
				next, e := walk(links[key], stack)
				delete(active, key)
				if e != nil {
					return nil, e
				}
				stack = next
			} else {
				stack = append(stack, part)
			}
		}
		return stack, nil
	}
	parent := []string{}
	if p := path.Dir(name); p != "." {
		parent = strings.Split(p, "/")
	}
	stack, e := walk(target, parent)
	if e != nil {
		return "", nil, e
	}
	resolved := strings.Join(stack, "/")
	if !safeArchivePath(resolved) {
		return "", nil, fmt.Errorf("unsafe repository link target")
	}
	return resolved, dependencies, nil
}
func createLinks(root string, links map[string]string) error {
	created := map[string]bool{}
	var create func(string) error
	create = func(name string) error {
		if created[name] {
			return nil
		}
		target := links[name]
		_, dependencies, e := traceLink(name, target, links)
		if e != nil {
			return e
		}
		for _, dependency := range dependencies {
			if e := create(dependency); e != nil {
				return e
			}
		}
		dest := filepath.Join(root, filepath.FromSlash(name))
		if e := os.MkdirAll(filepath.Dir(dest), 0755); e != nil {
			return e
		}
		if e := os.Symlink(filepath.FromSlash(target), dest); e != nil {
			return fmt.Errorf("create internal repository link %s: %w", name, e)
		}
		created[name] = true
		return nil
	}
	for name := range links {
		if e := create(name); e != nil {
			return e
		}
	}
	return nil
}
