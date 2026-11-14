// Package domain holds the health module's entities and the interface its usecase
// depends on. Standard library only.
package domain

import "context"

// Pinger is anything that can tell whether a service responds.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Dependency is a service the API needs, under the name the health check reports.
// An Optional dependency (the AI service, NFR-10.2) that does not respond leaves the API
// degraded, not down.
type Dependency struct {
	Name     string
	Pinger   Pinger
	Optional bool
}

// Result is the outcome of pinging one dependency. Err is nil when it responded.
type Result struct {
	Name     string
	Optional bool
	Err      error
}

// Report holds one Result per dependency, in the order the dependencies were given.
type Report struct {
	Results []Result
}

// Healthy reports whether every required dependency responded.
func (r Report) Healthy() bool {
	return len(r.Down()) == 0
}

// Down returns the names of the required dependencies that did not respond, in order.
func (r Report) Down() []string {
	return r.failed(false)
}

// Degraded returns the names of the optional dependencies that did not respond, in order.
func (r Report) Degraded() []string {
	return r.failed(true)
}

// Responded reports whether the dependency called name responded. A name the report does
// not hold counts as not responding.
func (r Report) Responded(name string) bool {
	for _, res := range r.Results {
		if res.Name == name {
			return res.Err == nil
		}
	}
	return false
}

func (r Report) failed(optional bool) []string {
	var names []string
	for _, res := range r.Results {
		if res.Err != nil && res.Optional == optional {
			names = append(names, res.Name)
		}
	}
	return names
}
