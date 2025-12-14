package executor

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// PTY manages a pseudo-terminal and the process running in it.
type PTY struct {
	cmd      *exec.Cmd
	pty      *os.File
	mu       sync.Mutex
	started  bool
	exitCode int
	exited   bool
	exitCh   chan struct{}
}

// NewPTY creates a new PTY manager.
func NewPTY(name string, args ...string) *PTY {
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()

	return &PTY{
		cmd:    cmd,
		exitCh: make(chan struct{}),
	}
}

// Start starts the process in a new PTY.
func (p *PTY) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return nil
	}

	ptmx, err := pty.Start(p.cmd)
	if err != nil {
		return err
	}

	p.pty = ptmx
	p.started = true

	// Monitor for exit
	go p.wait()

	return nil
}

// wait monitors the process for exit.
func (p *PTY) wait() {
	err := p.cmd.Wait()

	p.mu.Lock()
	p.exited = true
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			p.exitCode = exitErr.ExitCode()
		} else {
			p.exitCode = 1
		}
	} else {
		p.exitCode = 0
	}
	p.mu.Unlock()

	close(p.exitCh)
}

// Read reads from the PTY.
func (p *PTY) Read(buf []byte) (int, error) {
	return p.pty.Read(buf)
}

// Write writes to the PTY.
func (p *PTY) Write(buf []byte) (int, error) {
	return p.pty.Write(buf)
}

// Resize resizes the PTY.
func (p *PTY) Resize(cols, rows uint16) error {
	return pty.Setsize(p.pty, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
}

// Signal sends a signal to the process.
func (p *PTY) Signal(sig syscall.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd.Process == nil {
		return nil
	}

	return p.cmd.Process.Signal(sig)
}

// ExitCh returns a channel that is closed when the process exits.
func (p *PTY) ExitCh() <-chan struct{} {
	return p.exitCh
}

// ExitCode returns the exit code of the process.
// Only valid after ExitCh is closed.
func (p *PTY) ExitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCode
}

// Close closes the PTY.
func (p *PTY) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.pty != nil {
		return p.pty.Close()
	}
	return nil
}

// Reader returns an io.Reader for the PTY output.
func (p *PTY) Reader() io.Reader {
	return p.pty
}

// Writer returns an io.Writer for the PTY input.
func (p *PTY) Writer() io.Writer {
	return p.pty
}
