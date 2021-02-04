package types

import "time"

type PreflightResult struct {
	Result      string     `json:"result"`
	CreatedAt   *time.Time `json:"createdAt"`
	ClusterID   string     `json:"clusterId"`
	AppSlug     string     `json:"appSlug"`
	ClusterSlug string     `json:"clusterSlug"`
	AppID       string     `json:"appId"`
}
