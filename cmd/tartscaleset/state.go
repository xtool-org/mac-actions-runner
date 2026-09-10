package main

import "sync"

type runnerState struct {
	mu      sync.Mutex
	runners map[string]*runnerVM
}

func newRunnerState() runnerState {
	return runnerState{runners: make(map[string]*runnerVM)}
}

func (s *runnerState) add(runner *runnerVM) {
	s.mu.Lock()
	s.runners[runner.name] = runner
	s.mu.Unlock()
}

func (s *runnerState) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.runners)
}

func (s *runnerState) markBusy(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	runner, ok := s.runners[name]
	if ok {
		runner.busy = true
	}
	return ok
}

func (s *runnerState) take(name string) (*runnerVM, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	runner, ok := s.runners[name]
	if ok {
		delete(s.runners, name)
	}
	return runner, ok
}

func (s *runnerState) takeAll() []*runnerVM {
	s.mu.Lock()
	defer s.mu.Unlock()
	runners := make([]*runnerVM, 0, len(s.runners))
	for name, runner := range s.runners {
		runners = append(runners, runner)
		delete(s.runners, name)
	}
	return runners
}
