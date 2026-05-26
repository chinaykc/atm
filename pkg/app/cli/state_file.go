package cli

import (
	"encoding/json"
	"os"
	"strings"
)

type atmStateFile struct {
	Tasks map[string]atmTaskState `json:"tasks"`
}

type atmTaskState struct {
	Status             string   `json:"status"`
	SourcePromptHash   string   `json:"sourcePromptHash"`
	RenderedPromptHash string   `json:"renderedPromptHash"`
	Report             string   `json:"report"`
	Logs               []string `json:"logs"`
	Orphan             bool     `json:"orphan"`
}

func readStateFile(path string) (atmStateFile, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return atmStateFile{}, false, nil
		}
		return atmStateFile{}, false, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return atmStateFile{}, false, nil
	}
	var state atmStateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return atmStateFile{}, true, err
	}
	return state, true, nil
}

func stateTaskIDs(state atmStateFile) map[string]bool {
	ids := make(map[string]bool, len(state.Tasks))
	for id := range state.Tasks {
		ids[id] = true
	}
	return ids
}
