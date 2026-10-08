// Package domain holds the health module's entities and the interface its usecase
// depends on. Standard library only.
package domain

import "context"

// Pinger is anything that can tell whether a service responds.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Dependency is a service the API needs, under the name the health check reports.
type Dependency struct {
	Name   string
	Pinger Pinger
}

// Result is the outcome of pinging one dependency. Err is nil when it responded.
type Result struct {
	Name string
	Err  error
}

// Report holds one Result per dependency, in the order the dependencies were given.
type Report struct {
	Results []Result
}

// Healthy reports whether every dependency responded.
func (r Report) Healthy() bool {
	return len(r.Down()) == 0
}

// Down returns the names of the dependencies that did not respond, in order.
func (r Report) Down() []string {
	var down []string
	for _, res := range r.Results {
		if res.Err != nil {
			down = append(down, res.Name)
		}
	}
	return down
}
