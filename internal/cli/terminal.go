package cli

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// Terminal manages raw terminal mode and signal handling.
type Terminal struct {
	fd       int
	oldState *term.State
}

// NewTerminal creates a new terminal manager.
func NewTerminal() *Terminal {
	return &Terminal{
		fd: int(os.Stdin.Fd()),
	}
}

// MakeRaw puts the terminal into raw mode.
func (t *Terminal) MakeRaw() error {
	state, err := term.MakeRaw(t.fd)
	if err != nil {
		return err
	}
	t.oldState = state
	return nil
}

// Restore restores the terminal to its previous state.
func (t *Terminal) Restore() error {
	if t.oldState == nil {
		return nil
	}
	return term.Restore(t.fd, t.oldState)
}

// GetSize returns the current terminal size.
func (t *Terminal) GetSize() (cols, rows int, err error) {
	return term.GetSize(t.fd)
}

// IsTerminal returns true if stdin is a terminal.
func (t *Terminal) IsTerminal() bool {
	return term.IsTerminal(t.fd)
}

// ResizeHandler returns a channel that receives signals when the terminal is resized.
// The caller should close the returned channel when done.
func ResizeHandler() chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	return ch
}

// InterruptHandler returns a channel that receives interrupt signals.
func InterruptHandler() chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	return ch
}

// StopResizeHandler stops listening for resize signals.
func StopResizeHandler(ch chan os.Signal) {
	signal.Stop(ch)
	close(ch)
}

// StopInterruptHandler stops listening for interrupt signals.
func StopInterruptHandler(ch chan os.Signal) {
	signal.Stop(ch)
	close(ch)
}
