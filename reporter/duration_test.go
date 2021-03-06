// NOTE: Fail on only macos-latest of GitHub Actions by duration errors.
// +build !darwin

package reporter

import (
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestTestContext(t *testing.T) {
	t.Run("serial", func(t *testing.T) {
		ctx := newTestContext(WithMaxParallel(1))
		if expect, got := 1, ctx.running; got != expect {
			t.Errorf("expected %d but got %d", expect, got)
		}

		now := time.Now()
		duration := 100 * time.Millisecond
		done := make(chan struct{})
		go func() {
			time.Sleep(duration)
			if expect, got := int64(1), ctx.waitings(); got != expect {
				t.Errorf("expected %d but got %d", expect, got)
			}
			ctx.release()
			close(done)
		}()

		ctx.waitParallel() // wait goroutine function
		if expect, got := duration, time.Since(now).Truncate(10*time.Millisecond); got != expect {
			t.Errorf("expected %s but got %s", expect, got)
		}
		if expect, got := int64(0), ctx.waitings(); got != expect {
			t.Errorf("expected %d but got %d", expect, got)
		}

		<-done
	})
	t.Run("parallel", func(t *testing.T) {
		ctx := newTestContext(WithMaxParallel(2))
		if expect, got := 1, ctx.running; got != expect {
			t.Errorf("expected %d but got %d", expect, got)
		}

		now := time.Now()
		duration := 100 * time.Millisecond
		done := make(chan struct{})
		go func() {
			time.Sleep(duration)
			if expect, got := int64(0), ctx.waitings(); got != expect {
				t.Errorf("expected %d but got %d", expect, got)
			}
			ctx.release()
			close(done)
		}()

		ctx.waitParallel() // not wait goroutine function (run in parallel)
		if expect, got := time.Duration(0), time.Since(now).Truncate(10*time.Millisecond); got != expect {
			t.Errorf("expected %s but got %s", expect, got)
		}
		if expect, got := int64(0), ctx.waitings(); got != expect {
			t.Errorf("expected %d but got %d", expect, got)
		}

		<-done
	})
}

func TestRun(t *testing.T) {
	tests := map[string]struct {
		maxParallel            int
		fs                     []func(r Reporter)
		expect                 result
		expectSerialDuration   time.Duration
		expectParallelDuration time.Duration
	}{
		"all tests passed": {
			fs: []func(r Reporter){
				func(r Reporter) {},
				func(r Reporter) {},
			},
			expect: result{
				Children: []result{
					{},
					{},
				},
			},
		},
		"child failed": {
			fs: []func(r Reporter){
				func(r Reporter) {},
				func(r Reporter) {
					r.Fail()
				},
			},
			expect: result{
				Failed: true,
				Children: []result{
					{},
					{
						Failed: true,
					},
				},
			},
		},
		"child skipped": {
			fs: []func(r Reporter){
				func(r Reporter) {},
				func(r Reporter) {
					r.SkipNow()
				},
			},
			expect: result{
				Children: []result{
					{},
					{
						Skipped: true,
					},
				},
			},
		},
		`child panic("panic!")`: {
			fs: []func(r Reporter){
				func(r Reporter) {},
				func(r Reporter) {
					panic("panic!")
				},
			},
			expect: result{
				Failed: true,
				Children: []result{
					{},
					{
						Failed: true,
						Logs:   []string{"panic!"},
					},
				},
			},
		},
		"child panic(nil)": {
			fs: []func(r Reporter){
				func(r Reporter) {},
				func(r Reporter) {
					panic(nil)
				},
			},
			expect: result{
				Failed: true,
				Children: []result{
					{},
					{
						Failed: true,
						Logs:   []string{"test executed panic(nil) or runtime.Goexit"},
					},
				},
			},
		},
		"child runtime.Goexit()": {
			fs: []func(r Reporter){
				func(r Reporter) {},
				func(r Reporter) {
					runtime.Goexit()
				},
			},
			expect: result{
				Failed: true,
				Children: []result{
					{},
					{
						Failed: true,
						Logs:   []string{"test executed panic(nil) or runtime.Goexit"},
					},
				},
			},
		},
		"run all tests in parallel": {
			maxParallel: 3,
			fs: []func(r Reporter){
				func(r Reporter) {
					time.Sleep(100 * time.Millisecond)
				},
				func(r Reporter) {
					time.Sleep(100 * time.Millisecond)
				},
				func(r Reporter) {
					time.Sleep(100 * time.Millisecond)
				},
			},
			expect: result{
				Children: []result{
					{},
					{},
					{},
				},
			},
			expectSerialDuration:   300 * time.Millisecond,
			expectParallelDuration: 100 * time.Millisecond,
		},
		"run tests in parallel (max number of concurrent executions is 2)": {
			maxParallel: 2,
			fs: []func(r Reporter){
				func(r Reporter) {
					time.Sleep(100 * time.Millisecond)
				},
				func(r Reporter) {
					time.Sleep(100 * time.Millisecond)
				},
				func(r Reporter) {
					time.Sleep(100 * time.Millisecond)
				},
			},
			expect: result{
				Children: []result{
					{},
					{},
					{},
				},
			},
			expectSerialDuration:   300 * time.Millisecond,
			expectParallelDuration: 200 * time.Millisecond,
		},
	}
	for name, test := range tests {
		test := test
		opts := []Option{}
		if test.maxParallel != 0 {
			opts = append(opts, WithMaxParallel(test.maxParallel))
		}
		t.Run(name, func(t *testing.T) {
			t.Run("serial", func(t *testing.T) {
				start := time.Now()
				r := run(func(r Reporter) {
					for i, f := range test.fs {
						f := f
						r.Run(strconv.Itoa(i), func(r Reporter) {
							f(r)
						})
					}
				}, opts...)
				duration := time.Since(start)
				if diff := cmp.Diff(test.expect, ignoreStackTrace(collectResult(r))); diff != "" {
					t.Errorf("result mismatch (-want +got):\n%s", diff)
				}
				if expect, got := test.expectSerialDuration, duration.Truncate(50*time.Millisecond); got != expect {
					t.Errorf("expected %s but got %s", expect, got)
				}
			})
			t.Run("parallel", func(t *testing.T) {
				start := time.Now()
				r := run(func(r Reporter) {
					for i, f := range test.fs {
						f := f
						r.Run(strconv.Itoa(i), func(r Reporter) {
							r.Parallel()
							f(r)
						})
					}
				}, opts...)
				duration := time.Since(start)
				if diff := cmp.Diff(test.expect, ignoreStackTrace(collectResult(r))); diff != "" {
					t.Errorf("result mismatch (-want +got):\n%s", diff)
				}
				if expect, got := test.expectParallelDuration, duration.Truncate(50*time.Millisecond); got != expect {
					t.Errorf("expected %s but got %s", expect, got)
				}
			})
		})
	}
}
