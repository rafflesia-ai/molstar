package cli

import "time"

type stageReport struct {
	Name       string `json:"name"`
	Detail     string `json:"detail,omitempty"`
	OK         bool   `json:"ok"`
	StartedAt  string `json:"started_at"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (r *renderReport) finishStage(name string, detail string, start time.Time, err error) {
	stage := stageReport{
		Name:       name,
		Detail:     detail,
		OK:         err == nil,
		StartedAt:  start.UTC().Format(time.RFC3339Nano),
		DurationMS: time.Since(start).Milliseconds(),
	}
	if err != nil {
		stage.Error = err.Error()
	}
	r.Stages = append(r.Stages, stage)
}
