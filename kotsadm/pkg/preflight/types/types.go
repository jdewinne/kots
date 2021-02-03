package types

import "time"

type PreflightResult struct {
	Result       string     `json:"result"`
	CreatedAt    *time.Time `json:"createdAt"`
	AppSlug      string     `json:"appSlug"`
	ClusterSlug  string     `json:"clusterSlug"`
	License      string     `json:"license"`
	InstallState string     `json:"installState"`
}
