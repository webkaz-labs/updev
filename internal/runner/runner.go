package runner

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
)

type Result struct {
	Stdout string
	Stderr string
	Code   int
	Err    error
}

type Runner interface {
	LookPath(name string) (string, error)
	Run(ctx context.Context, name string, args ...string) Result
}

type Local struct{}

func (Local) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (Local) Run(ctx context.Context, name string, args ...string) Result {
	return (Local{}).RunStreaming(ctx, nil, nil, name, args...)
}

func (Local) RunStreaming(ctx context.Context, stdout io.Writer, stderr io.Writer, name string, args ...string) Result {
	command := exec.CommandContext(ctx, name, args...)
	var outBuffer bytes.Buffer
	var errBuffer bytes.Buffer
	if stdout == nil {
		command.Stdout = &outBuffer
	} else {
		command.Stdout = io.MultiWriter(&outBuffer, stdout)
	}
	if stderr == nil {
		command.Stderr = &errBuffer
	} else {
		command.Stderr = io.MultiWriter(&errBuffer, stderr)
	}
	err := command.Run()
	result := Result{
		Stdout: strings.TrimSpace(outBuffer.String()),
		Stderr: strings.TrimSpace(errBuffer.String()),
		Err:    err,
	}
	if err == nil {
		return result
	}
	if exit, ok := err.(*exec.ExitError); ok {
		result.Code = exit.ExitCode()
		return result
	}
	result.Code = 1
	return result
}
