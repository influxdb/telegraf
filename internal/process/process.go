package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/influxdata/telegraf"
)

// Process is a long-running process manager that will restart processes if they stop.
type Process struct {
	Cmd          *exec.Cmd
	Stdin        io.WriteCloser
	Stdout       io.ReadCloser
	Stderr       io.ReadCloser
	ReadStdoutFn func(io.Reader)
	ReadStderrFn func(io.Reader)
	RestartDelay time.Duration
	StopOnError  bool
	Log          telegraf.Logger

	name       string
	args       []string
	envs       []string
	pid        int32
	cancel     context.CancelFunc
	mainLoopWg sync.WaitGroup

	sync.Mutex
}

// New creates a new process wrapper
func New(command, envs []string) (*Process, error) {
	if len(command) == 0 {
		return nil, errors.New("no command")
	}

	p := &Process{
		RestartDelay: 5 * time.Second,
		name:         command[0],
		args:         make([]string, 0),
		envs:         envs,
	}

	if len(command) > 1 {
		p.args = command[1:]
	}

	return p, nil
}

// Start the process. A &Process can only be started once. It will restart itself
// as necessary.
func (p *Process) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	if err := p.cmdStart(); err != nil {
		return err
	}

	p.mainLoopWg.Add(1)
	go func() {
		defer p.mainLoopWg.Done()
		if err := p.cmdLoop(ctx); err != nil {
			p.Log.Errorf("Process quit with message: %v", err)
		}
	}()

	return nil
}

// Stop is called when the process isn't needed anymore
func (p *Process) Stop() {
	if p.cancel != nil {
		// signal our intent to shut down and not restart the process
		p.cancel()
	}
	// close stdin so the app can shut down gracefully.
	if err := p.Stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		p.Log.Errorf("Stdin closed with message: %v", err)
	}
	p.mainLoopWg.Wait()
}

func (p *Process) Pid() int {
	pid := atomic.LoadInt32(&p.pid)
	return int(pid)
}

func (p *Process) State() (state *os.ProcessState, running bool) {
	p.Lock()
	defer p.Unlock()

	return p.Cmd.ProcessState, p.Cmd.ProcessState.ExitCode() == -1
}

func (p *Process) cmdStart() error {
	p.Cmd = exec.Command(p.name, p.args...)

	if len(p.envs) > 0 {
		p.Cmd.Env = append(os.Environ(), p.envs...)
	}

	var err error
	p.Stdin, err = p.Cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("error opening stdin pipe: %w", err)
	}

	p.Stdout, err = p.Cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error opening stdout pipe: %w", err)
	}

	p.Stderr, err = p.Cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("error opening stderr pipe: %w", err)
	}

	p.Log.Infof("Starting process: %s %s", p.name, p.args)

	if err := p.Cmd.Start(); err != nil {
		return fmt.Errorf("error starting process: %w", err)
	}
	atomic.StoreInt32(&p.pid, int32(p.Cmd.Process.Pid))
	return nil
}

// cmdLoop watches an already running process, restarting it when appropriate.
func (p *Process) cmdLoop(ctx context.Context) error {
	for {
		err := p.cmdWait(ctx)
		if err != nil && p.StopOnError {
			return err
		}
		if isQuitting(ctx) {
			p.Log.Infof("Process %s shut down", p.Cmd.Path)
			return nil
		}

		p.Log.Errorf("Process %s exited: %v", p.Cmd.Path, err)
		p.Log.Infof("Restarting in %s...", p.RestartDelay)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(p.RestartDelay):
			// Continue the loop and restart the process
			if err := p.cmdStart(); err != nil {
				return err
			}
		}
	}
}

// cmdWait waits for the process to finish.
func (p *Process) cmdWait(ctx context.Context) error {
	var wg sync.WaitGroup

	if p.ReadStdoutFn == nil {
		p.ReadStdoutFn = defaultReadPipe
	}
	if p.ReadStderrFn == nil {
		p.ReadStderrFn = defaultReadPipe
	}

	processCtx, processCancel := context.WithCancel(context.Background())
	defer processCancel()

	wg.Add(1)
	go func() {
		p.ReadStdoutFn(p.Stdout)
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		p.ReadStderrFn(p.Stderr)
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		select {
		case <-ctx.Done():
			p.gracefulStop(processCtx, p.Cmd, 5*time.Second)
		case <-processCtx.Done():
		}
		wg.Done()
	}()

	p.Lock()
	err := p.Cmd.Wait()
	p.Unlock()
	processCancel()
	wg.Wait()
	return err
}

func isQuitting(ctx context.Context) bool {
	return ctx.Err() != nil
}

func defaultReadPipe(r io.Reader) {
	//nolint:errcheck // Discarding the data, no need to handle an error
	io.Copy(io.Discard, r)
}
