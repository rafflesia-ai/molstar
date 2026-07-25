package job

import "fmt"

const LatestVersion = 1

func MigrateToLatest(j Job) (Job, error) {
	if j.Version == LatestVersion {
		return j, nil
	}
	if j.Version == 0 {
		return Job{}, fmt.Errorf("version is required")
	}
	return Job{}, fmt.Errorf("unsupported job version %d; latest supported version is %d", j.Version, LatestVersion)
}
