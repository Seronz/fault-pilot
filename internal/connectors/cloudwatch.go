package connectors

import "context"

type CloudWatchAdapter struct{}

func NewCloudWatchAdapter() *CloudWatchAdapter                            { return &CloudWatchAdapter{} }
func (c *CloudWatchAdapter) Source() string                               { return "cloudwatch" }
func (c *CloudWatchAdapter) Poll(ctx context.Context) ([]Incident, error) { return []Incident{}, nil }
