package term

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// Process is an interactive program attached to a terminal.
type Process interface {
	io.ReadWriter
	Resize(cols, rows int) error
	// Wait returns the exit code once the process has ended.
	Wait() (int, error)
	// Kill ends the process (and the PTY).
	Kill() error
}

// Starter starts argv[0] with argv[1:] in a terminal of the given size.
type Starter func(ctx context.Context, argv []string, cols, rows int) (Process, error)

// StartPTY runs argv in a new PTY with TERM=xterm-256color.
func StartPTY(ctx context.Context, argv []string, cols, rows int) (Process, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // argv from sshx.Client.Args (quoted remote command)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}) //nolint:gosec // sizes validated (≤ 1000)
	if err != nil {
		return nil, err
	}
	return &ptyProcess{cmd: cmd, f: f}, nil
}

type ptyProcess struct {
	cmd *exec.Cmd
	f   *os.File
}

func (p *ptyProcess) Read(b []byte) (int, error) {
	n, err := p.f.Read(b)
	// Linux reports EIO on the master once the child side is closed.
	if errors.Is(err, syscall.EIO) {
		err = io.EOF
	}
	return n, err
}

func (p *ptyProcess) Write(b []byte) (int, error) { return p.f.Write(b) }

func (p *ptyProcess) Resize(cols, rows int) error {
	return pty.Setsize(p.f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}) //nolint:gosec // validated
}

func (p *ptyProcess) Wait() (int, error) {
	err := p.cmd.Wait()
	_ = p.f.Close()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

func (p *ptyProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}
