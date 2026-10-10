package system

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type PanelActivity struct {
	Session      string    `json:"session"`
	LastActivity time.Time `json:"last_activity"`
	Event        string    `json:"event"`
}

// Expose event kinds and tool names only, never arguments, tool output, or model text.
func nativePanelActivity(configPath, stateDir string) map[string]PanelActivity {
	result := map[string]PanelActivity{}
	b, e := os.ReadFile(configPath)
	if e != nil {
		return result
	}
	var config Config
	if json.Unmarshal(b, &config) != nil || config.Backend != "native-windows" {
		return result
	}
	b, e = os.ReadFile(filepath.Join(stateDir, "state.json"))
	if e != nil {
		return result
	}
	var state State
	if json.Unmarshal(b, &state) != nil {
		return result
	}
	type candidate struct {
		path string
		at   time.Time
	}
	var files []candidate
	filepath.WalkDir(filepath.Join(config.NativeRoot, "codex-runtime", "sessions"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") {
			if info, e := d.Info(); e == nil {
				files = append(files, candidate{path, info.ModTime()})
			}
		}
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].at.After(files[j].at) })
	if len(files) > 64 {
		files = files[:64]
	}
	for _, file := range files {
		f, e := os.Open(file.path)
		if e != nil {
			continue
		}
		first, e := bufio.NewReader(io.LimitReader(f, 1<<20)).ReadBytes('\n')
		if e != nil {
			f.Close()
			continue
		}
		var meta struct {
			Payload struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if json.Unmarshal(first, &meta) != nil {
			f.Close()
			continue
		}
		var job *Job
		for _, j := range state.Jobs {
			if j.Stage != "running" || !safeID.MatchString(j.ID) {
				continue
			}
			want := filepath.Join(config.NativeRoot, "jobs", j.ID, "workspace")
			if strings.EqualFold(filepath.Clean(meta.Payload.CWD), filepath.Clean(want)) && (j.RunStartedAt.IsZero() || !file.at.Before(j.RunStartedAt)) {
				job = j
				break
			}
		}
		if job == nil {
			f.Close()
			continue
		}
		if _, exists := result[job.ID]; exists {
			f.Close()
			continue
		}
		info, _ := f.Stat()
		offset := info.Size() - 32768
		if offset < 0 {
			offset = 0
		}
		f.Seek(offset, io.SeekStart)
		activity := PanelActivity{meta.Payload.ID, file.at, "Session activity"}
		scanner := bufio.NewScanner(io.LimitReader(f, 32768))
		scanner.Buffer(make([]byte, 4096), 65536)
		for scanner.Scan() {
			var event struct {
				Type    string `json:"type"`
				Payload struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"payload"`
			}
			if json.Unmarshal(scanner.Bytes(), &event) != nil {
				continue
			}
			switch event.Payload.Type {
			case "function_call":
				activity.Event = "Tool: " + clip(event.Payload.Name, 80)
			case "function_call_output":
				activity.Event = "Tool result received"
			case "agent_message":
				activity.Event = "Agent message"
			case "reasoning":
				activity.Event = "Reasoning"
			}
		}
		f.Close()
		result[job.ID] = activity
	}
	return result
}
