// SiYuan multi-workspace launcher for Docker.
//
// It reads a JSON config and starts one kernel process per workspace.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type workspaceConfig struct {
	Name string `json:"-"`
	Path string `json:"path"`
	Port int    `json:"port"`
}

const defaultWorkspaceRoot = "/siyuan/workspaces"
const defaultWorkspaceBasePort = 30000

type childProcess struct {
	Name string
	Cmd  *exec.Cmd
	Done chan struct{}
}

type childExit struct {
	Name string
	Code int
}

func main() {
	configPath := flag.String("config", os.Getenv("SIYUAN_WORKSPACES_CONFIG"), "path to workspaces json config")
	kernelPath := flag.String("kernel", "/opt/siyuan/kernel", "path to SiYuan kernel binary")
	workspaceRoot := flag.String("workspace-root", getEnvDefault("SIYUAN_WORKSPACES_ROOT", defaultWorkspaceRoot), "root directory for simplified workspace config")
	workspaceBasePort := flag.Int("workspace-base-port", getEnvIntDefault("SIYUAN_WORKSPACE_BASE_PORT", defaultWorkspaceBasePort), "base port for simplified workspace config")
	flag.Parse()

	if err := run(*configPath, *kernelPath, *workspaceRoot, *workspaceBasePort, sanitizeKernelArgs(flag.Args())); err != nil {
		fmt.Fprintf(os.Stderr, "siyuan-multi: %s\n", err)
		os.Exit(1)
	}
}

func run(configPath, kernelPath, workspaceRoot string, workspaceBasePort int, commonArgs []string) error {
	if configPath == "" {
		return errors.New("missing config path")
	}
	if kernelPath == "" {
		return errors.New("missing kernel path")
	}

	workspaces, err := loadConfig(configPath, workspaceRoot, workspaceBasePort)
	if err != nil {
		return err
	}
	if len(workspaces) == 0 {
		return fmt.Errorf("no workspace configured in %s", configPath)
	}

	if err := validateConfig(workspaces); err != nil {
		return err
	}

	children := make([]childProcess, 0, len(workspaces))
	exited := make(chan childExit, len(workspaces))
	for _, workspace := range workspaces {
		if err := os.MkdirAll(workspace.Path, 0755); err != nil {
			return fmt.Errorf("create workspace [%s] failed: %w", workspace.Path, err)
		}

		args := append([]string{}, commonArgs...)
		args = append(args,
			"--workspace", workspace.Path,
			"--port", fmt.Sprintf("%d", workspace.Port),
		)

		cmd := exec.Command(kernelPath, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Start(); err != nil {
			stopChildren(children, 10*time.Second)
			return fmt.Errorf("start workspace [%s] on port [%d] failed: %w", workspace.Name, workspace.Port, err)
		}

		child := childProcess{Name: workspace.Name, Cmd: cmd, Done: make(chan struct{})}
		children = append(children, child)
		fmt.Printf("started workspace [%s] path [%s] port [%d] pid [%d]\n", workspace.Name, workspace.Path, workspace.Port, cmd.Process.Pid)

		go func(child childProcess) {
			_ = child.Cmd.Wait()
			code := 0
			if child.Cmd.ProcessState != nil {
				code = child.Cmd.ProcessState.ExitCode()
			}
			close(child.Done)
			exited <- childExit{Name: child.Name, Code: code}
		}(child)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case sig := <-signals:
		fmt.Printf("received signal [%s], stopping kernels\n", sig)
		return stopChildren(children, 15*time.Second)
	case exit := <-exited:
		fmt.Printf("workspace [%s] exited with code [%d], stopping remaining kernels\n", exit.Name, exit.Code)
		_ = stopChildren(children, 15*time.Second)
		if exit.Code != 0 {
			os.Exit(exit.Code)
		}
		return nil
	}
}

func loadConfig(configPath, workspaceRoot string, workspaceBasePort int) ([]workspaceConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config [%s] failed: %w", configPath, err)
	}

	names := []string{}
	if err := json.Unmarshal(data, &names); err != nil {
		return nil, fmt.Errorf("decode config [%s] failed: %w", configPath, err)
	}

	workspaceRoot = filepath.Clean(workspaceRoot)
	workspaces := make([]workspaceConfig, 0, len(names))
	for i, name := range names {
		name = strings.TrimSpace(name)
		workspaces = append(workspaces, workspaceConfig{
			Name: name,
			Path: filepath.Join(workspaceRoot, name),
			Port: workspaceBasePort + i,
		})
	}
	return workspaces, nil
}

func getEnvDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func getEnvIntDefault(name string, fallback int) int {
	if value := os.Getenv(name); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func validateConfig(workspaces []workspaceConfig) error {
	names := map[string]struct{}{}
	ports := map[int]string{}
	paths := map[string]string{}
	for _, workspace := range workspaces {
		if strings.TrimSpace(workspace.Name) == "" {
			return errors.New("workspace name must not be empty")
		}
		if _, ok := names[workspace.Name]; ok {
			return fmt.Errorf("workspace [%s] is duplicated", workspace.Name)
		}
		names[workspace.Name] = struct{}{}

		if strings.TrimSpace(workspace.Path) == "" {
			return fmt.Errorf("workspace [%s] path must not be empty", workspace.Name)
		}
		if !filepath.IsAbs(workspace.Path) {
			return fmt.Errorf("workspace [%s] path [%s] must be absolute", workspace.Name, workspace.Path)
		}
		if workspace.Port < 1 || workspace.Port > 65535 {
			return fmt.Errorf("workspace [%s] port [%d] is out of range", workspace.Name, workspace.Port)
		}

		cleanPath := filepath.Clean(workspace.Path)
		if previous, ok := paths[cleanPath]; ok {
			return fmt.Errorf("workspace [%s] and [%s] use the same path [%s]", previous, workspace.Name, cleanPath)
		}
		paths[cleanPath] = workspace.Name

		if previous, ok := ports[workspace.Port]; ok {
			return fmt.Errorf("workspace [%s] and [%s] use the same port [%d]", previous, workspace.Name, workspace.Port)
		}
		ports[workspace.Port] = workspace.Name
	}
	return nil
}

func sanitizeKernelArgs(args []string) []string {
	ret := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "/opt/siyuan/kernel" || arg == "kernel":
			continue
		case arg == "--workspace" || arg == "-workspace" || arg == "--port" || arg == "-port":
			i++
			continue
		case strings.HasPrefix(arg, "--workspace=") || strings.HasPrefix(arg, "-workspace="):
			continue
		case strings.HasPrefix(arg, "--port=") || strings.HasPrefix(arg, "-port="):
			continue
		default:
			ret = append(ret, arg)
		}
	}
	return ret
}

func stopChildren(children []childProcess, grace time.Duration) error {
	var firstErr error
	deadline := time.NewTimer(grace)
	defer deadline.Stop()

	for _, child := range children {
		if child.Cmd.Process == nil || isDone(child) {
			continue
		}
		if err := child.Cmd.Process.Signal(syscall.SIGTERM); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("stop workspace [%s] failed: %w", child.Name, err)
		}
	}

	for _, child := range children {
		if isDone(child) {
			continue
		}
		select {
		case <-child.Done:
		case <-deadline.C:
			for _, remaining := range children {
				if remaining.Cmd.Process == nil || isDone(remaining) {
					continue
				}
				_ = remaining.Cmd.Process.Kill()
			}
			for _, remaining := range children {
				if !isDone(remaining) {
					<-remaining.Done
				}
			}
			return firstErr
		}
	}
	return firstErr
}

func isDone(child childProcess) bool {
	select {
	case <-child.Done:
		return true
	default:
		return false
	}
}
