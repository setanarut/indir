package indir

import (
	"encoding/json"
	"os"
)

// segmentState is the JSON-serialisable form of a segment.
type segmentState struct {
	Index      int   `json:"index"`
	Start      int64 `json:"start"`
	End        int64 `json:"end"`
	Downloaded int64 `json:"downloaded"`
}

// downloadState is persisted to disk so a download can be resumed later.
type downloadState struct {
	URL       string         `json:"url"`
	TotalSize int64          `json:"total_size"`
	Segments  []segmentState `json:"segments"`
}

func (s *downloadState) toSegments() []*segment {
	segs := make([]*segment, len(s.Segments))
	for i, ss := range s.Segments {
		segs[i] = &segment{
			Index:      ss.Index,
			Start:      ss.Start,
			End:        ss.End,
			Downloaded: ss.Downloaded,
		}
	}
	return segs
}

func loadState(path string) (*downloadState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s downloadState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func persistState(path string, s *downloadState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
