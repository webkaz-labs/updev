package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Result struct {
	Stdout string
	Stderr string
	Code   int
	Err    error
}

type ResultDetailOption struct {
	IncludeExitStatus bool
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
	return (Local{}).RunStreamingWithEnv(ctx, nil, stdout, stderr, name, args...)
}

func (Local) RunWithEnv(ctx context.Context, env []string, name string, args ...string) Result {
	return (Local{}).RunStreamingWithEnv(ctx, env, nil, nil, name, args...)
}

func (Local) RunStreamingWithEnv(ctx context.Context, env []string, stdout io.Writer, stderr io.Writer, name string, args ...string) Result {
	command := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		command.Env = append(os.Environ(), env...)
	}
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

func ResultDetail(result Result, fallback string, option ResultDetailOption) string {
	values := []string{result.Stderr, result.Stdout}
	if result.Err != nil {
		values = append(values, result.Err.Error())
	}
	if option.IncludeExitStatus && result.Code != 0 {
		values = append(values, fmt.Sprintf("exit status %d", result.Code))
	}
	values = append(values, fallback)
	return firstNonEmpty(values...)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
