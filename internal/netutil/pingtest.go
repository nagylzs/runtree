package netutil

import (
	"fmt"
	probing "github.com/prometheus-community/pro-bing"
	"time"
)

// PingTest sends ping requests to a host, waits until the given timeout and returns the stats.
func PingTest(hostname string, count int, timeout time.Duration) (*probing.Statistics, error) {
	p, err := probing.NewPinger(hostname)
	if err != nil {
		return nil, fmt.Errorf("failed to create pinger: %w", err)
	}
	p.Count = count
	p.Timeout = timeout
	err = p.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to run pinger: %w", err)
	}
	return p.Statistics(), nil
}
