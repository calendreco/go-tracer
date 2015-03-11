package tracer

import (
	"bytes"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/rcrowley/go-metrics"
)

// Tracer is a struct that implements the `metrics.Timer` interface and
// provides hooks for timing blocks of code.
type Tracer struct {
	path     string
	registry metrics.Registry
	timer    metrics.Timer
}

// A function that takes a Tracer
type TracedFunc func(*Tracer)

// TimeFunc times the function passed in, with the path of the current
// Tracer, appending ".pathComponent" to its trace path. The tracer
// passed to `tracedFunc` can be used to further add to the trace.
func (t *Tracer) TimeFunc(pathComponent string, tracedFunc TracedFunc) {
	tracer := t.GetOrRegister(pathComponent)
	tracer.Time(func() { tracedFunc(tracer) })
}

func (t *Tracer) GetOrRegister(pathComponent string) *Tracer {
	var buffer bytes.Buffer
	buffer.WriteString(t.path)
	buffer.WriteString(".")
	buffer.WriteString(pathComponent)
	path := buffer.String()
	tracer, _ := t.registry.GetOrRegister(path, tracerGenerator(path)).(*Tracer)
	return tracer
}

// Path of the Tracer
func (t *Tracer) Path() string {
	return t.path
}

func NewTracer(path string) *Tracer {
	return &Tracer{
		path,
		newTracerRegistry(),
		metrics.NewTimer(),
	}
}

func tracerGenerator(path string) func() *Tracer {
	return func() *Tracer {
		return NewTracer(path)
	}
}

// Fulfill `metrics.Timer` interface

func (t *Tracer) Count() int64                         { return t.timer.Count() }
func (t *Tracer) Max() int64                           { return t.timer.Max() }
func (t *Tracer) Mean() float64                        { return t.timer.Mean() }
func (t *Tracer) Min() int64                           { return t.timer.Min() }
func (t *Tracer) Percentile(p float64) float64         { return t.timer.Percentile(p) }
func (t *Tracer) Percentiles(pcts []float64) []float64 { return t.timer.Percentiles(pcts) }
func (t *Tracer) Rate1() float64                       { return t.timer.Rate1() }
func (t *Tracer) Rate5() float64                       { return t.timer.Rate5() }
func (t *Tracer) Rate15() float64                      { return t.timer.Rate15() }
func (t *Tracer) RateMean() float64                    { return t.timer.RateMean() }
func (t *Tracer) Snapshot() metrics.Timer              { return t.timer.Snapshot() }
func (t *Tracer) StdDev() float64                      { return t.timer.StdDev() }
func (t *Tracer) Sum() int64                           { return t.timer.Sum() }
func (t *Tracer) Time(f func())                        { t.timer.Time(f) }
func (t *Tracer) Update(d time.Duration)               { t.timer.Update(d) }
func (t *Tracer) UpdateSince(since time.Time)          { t.timer.UpdateSince(since) }
func (t *Tracer) Variance() float64                    { return t.timer.Variance() }

var _ metrics.Timer = &Tracer{}

func newTracerRegistry() *tracerRegistry {
	return &tracerRegistry{tracePaths: make(map[string]*Tracer)}
}

// A private `metrics.Registry` implementation. Will call all child
// Tracers when iterating using the `metrics.Registry#Each` method.
type tracerRegistry struct {
	tracePaths map[string]*Tracer
	mutex      sync.RWMutex
}

func (tr *tracerRegistry) registered() map[string]*Tracer {
	// No need for a write lock here, despite the confusing method name
	tr.mutex.RLock()
	defer tr.mutex.RUnlock()
	registeredTracers := make(map[string]*Tracer, len(tr.tracePaths))
	for path, t := range tr.tracePaths {
		registeredTracers[path] = t
	}
	return registeredTracers
}

func (tr *tracerRegistry) Each(f func(string, interface{})) {
	for path, tracer := range tr.registered() {
		f(path, tracer)
		tracer.registry.Each(f)
	}
}

func (tr *tracerRegistry) Get(path string) interface{} {
	t, _ := tr.get(path)
	return t
}

// Acquires read lock, no need to wrap this call
func (tr *tracerRegistry) get(path string) (*Tracer, bool) {
	tr.mutex.RLock()
	defer tr.mutex.RUnlock()
	t, ok := tr.tracePaths[path]
	return t, ok
}

func (tr *tracerRegistry) GetOrRegister(path string, i interface{}) interface{} {
	if tracer, ok := tr.get(path); ok {
		return tracer
	}
	var t *Tracer
	if v := reflect.ValueOf(i); v.Kind() == reflect.Func {
		t = v.Call(nil)[0].Interface().(*Tracer)
	}
	tr.register(path, t)
	return t
}

func (tr *tracerRegistry) Register(path string, i interface{}) error {
	t, ok := i.(*Tracer)
	if !ok {
		return errors.New("Cannot register non-Tracer with tracerRegistry")
	}

	return tr.register(path, t)
}

// Acquires write lock, no need to wrap this call
func (tr *tracerRegistry) register(path string, t *Tracer) error {
	tr.mutex.Lock()
	defer tr.mutex.Unlock()

	if _, ok := tr.tracePaths[path]; ok {
		return metrics.DuplicateMetric(path)
	}
	tr.tracePaths[path] = t
	return nil
}

// No-op
func (tr *tracerRegistry) RunHealthchecks() {}

func (tr *tracerRegistry) Unregister(path string) {
	tr.mutex.Lock()
	defer tr.mutex.Unlock()
	delete(tr.tracePaths, path)
}

func (tr *tracerRegistry) UnregisterAll() {
	tr.mutex.Lock()
	defer tr.mutex.Unlock()
	for path, _ := range tr.tracePaths {
		delete(tr.tracePaths, path)
	}
}

var _ metrics.Registry = &tracerRegistry{}
