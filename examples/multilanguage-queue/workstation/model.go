// Package workstation models one nonpreemptive FCFS server independently of HLA.
package workstation

import "fmt"

type Job struct {
	Sequence uint32  `json:"sequence"`
	Arrival  float64 `json:"arrival"`
	Service  uint32  `json:"service"`
}

type Completion struct {
	Job
	Start  float64 `json:"start"`
	Finish float64 `json:"finish"`
	Wait   float64 `json:"wait"`
}

type Server struct {
	available   float64
	lastArrival float64
	count       uint32
}

// Accept reserves service on arrival; no cancellation or preemption is modelled.
func (s *Server) Accept(job Job) (Completion, error) {
	if job.Service == 0 || job.Sequence != s.count+1 || job.Arrival < s.lastArrival {
		return Completion{}, fmt.Errorf("invalid or out-of-order job: %+v", job)
	}
	start := max(s.available, job.Arrival)
	row := Completion{Job: job, Start: start, Finish: start + float64(job.Service), Wait: start - job.Arrival}
	s.available, s.lastArrival, s.count = row.Finish, job.Arrival, job.Sequence
	return row, nil
}
