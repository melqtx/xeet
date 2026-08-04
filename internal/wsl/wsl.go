package wsl

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode/utf16"
)

var ErrUnavailable = errors.New("windows interoperability is unavailable")

type commandRunner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Do not return an ExitError containing captured process output. The
		// DPAPI process handles account-level session material.
		return nil, errors.New("windows command failed")
	}
	return stdout.Bytes(), nil
}

type bridge struct {
	goos      string
	lookupEnv func(string) (string, bool)
	stat      func(string) (os.FileInfo, error)
	lookPath  func(string) (string, error)
	runner    commandRunner
}

var systemBridge = bridge{
	goos:      runtime.GOOS,
	lookupEnv: os.LookupEnv,
	stat:      os.Stat,
	lookPath:  exec.LookPath,
	runner:    execRunner{},
}

// Active reports whether the current Linux process is running under WSL.
func Active() bool {
	return systemBridge.active()
}

func (b bridge) active() bool {
	if b.goos != "linux" {
		return false
	}
	if _, ok := b.lookupEnv("WSL_DISTRO_NAME"); ok {
		return true
	}
	_, err := b.stat("/proc/sys/fs/binfmt_misc/WSLInterop")
	return err == nil
}

// AppData resolves the current Windows user's roaming AppData directory and
// converts it to a path that the WSL process can open.
func AppData(ctx context.Context) (string, error) {
	return systemBridge.appData(ctx)
}

func (b bridge) appData(ctx context.Context) (string, error) {
	if !b.active() {
		return "", ErrUnavailable
	}
	cmdPath, err := b.lookPath("cmd.exe")
	if err != nil {
		return "", fmt.Errorf("%w: cmd.exe is not on PATH", ErrUnavailable)
	}
	out, err := b.runner.Run(ctx, cmdPath, []string{"/d", "/c", "echo %APPDATA%"}, nil)
	if err != nil {
		return "", fmt.Errorf("%w: could not query %%APPDATA%%", ErrUnavailable)
	}
	windowsPath := strings.TrimSpace(strings.ReplaceAll(string(out), "\r", ""))
	if windowsPath == "" || strings.Contains(windowsPath, "%APPDATA%") {
		return "", fmt.Errorf("%w: Windows did not return %%APPDATA%%", ErrUnavailable)
	}

	wslpath, err := b.lookPath("wslpath")
	if err != nil {
		return "", fmt.Errorf("%w: wslpath is not on PATH", ErrUnavailable)
	}
	out, err = b.runner.Run(ctx, wslpath, []string{"-u", windowsPath}, nil)
	if err != nil {
		return "", fmt.Errorf("%w: could not translate %%APPDATA%%", ErrUnavailable)
	}
	path := strings.TrimSpace(strings.ReplaceAll(string(out), "\r", ""))
	if path == "" || !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("%w: wslpath returned an invalid path", ErrUnavailable)
	}
	return path, nil
}

type dpapiRequest struct {
	Operation string `json:"operation"`
	Key       string `json:"key"`
	Payload   string `json:"payload"`
}

// Protect encrypts plaintext with Windows DPAPI in the current Windows user's
// security context. The plaintext is sent only over the child process's stdin.
func Protect(ctx context.Context, key string, plaintext []byte) ([]byte, error) {
	return systemBridge.dpapi(ctx, "protect", key, plaintext)
}

// Unprotect decrypts a DPAPI ciphertext in the current Windows user's security
// context. The returned plaintext exists only in process memory.
func Unprotect(ctx context.Context, key string, ciphertext []byte) ([]byte, error) {
	return systemBridge.dpapi(ctx, "unprotect", key, ciphertext)
}

func (b bridge) dpapi(ctx context.Context, operation, key string, payload []byte) ([]byte, error) {
	if !b.active() {
		return nil, ErrUnavailable
	}
	if operation != "protect" && operation != "unprotect" {
		return nil, errors.New("invalid DPAPI operation")
	}
	if !validKey(key) {
		return nil, errors.New("invalid DPAPI key name")
	}
	powershell, err := b.lookPath("powershell.exe")
	if err != nil {
		return nil, fmt.Errorf("%w: powershell.exe is not on PATH", ErrUnavailable)
	}
	request, err := json.Marshal(dpapiRequest{
		Operation: operation,
		Key:       key,
		Payload:   base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil {
		return nil, errors.New("could not prepare DPAPI request")
	}
	out, err := b.runner.Run(ctx, powershell, []string{
		"-NoLogo", "-NoProfile", "-NonInteractive",
		"-EncodedCommand", encodedDPAPIScript,
	}, request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, errors.New("windows DPAPI operation failed")
	}
	result, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil || len(result) == 0 {
		return nil, errors.New("windows DPAPI returned an invalid result")
	}
	return result, nil
}

func validKey(key string) bool {
	if key == "" || len(key) > 64 {
		return false
	}
	for _, r := range key {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func encodePowerShell(script string) string {
	encoded := utf16.Encode([]rune(script))
	raw := make([]byte, len(encoded)*2)
	for i, value := range encoded {
		raw[i*2] = byte(value)
		raw[i*2+1] = byte(value >> 8)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

var encodedDPAPIScript = encodePowerShell(`
$ErrorActionPreference = 'Stop'
try {
    $request = [Console]::In.ReadToEnd() | ConvertFrom-Json
    $payload = [Convert]::FromBase64String([string]$request.payload)
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try {
        $entropy = $sha.ComputeHash([Text.Encoding]::UTF8.GetBytes("xeet" + [char]0 + [string]$request.key))
    } finally {
        $sha.Dispose()
    }
    $scope = [System.Security.Cryptography.DataProtectionScope]::CurrentUser
    if ([string]$request.operation -eq 'protect') {
        $result = [System.Security.Cryptography.ProtectedData]::Protect($payload, $entropy, $scope)
    } elseif ([string]$request.operation -eq 'unprotect') {
        $result = [System.Security.Cryptography.ProtectedData]::Unprotect($payload, $entropy, $scope)
    } else {
        throw 'invalid operation'
    }
    [Console]::Out.Write([Convert]::ToBase64String($result))
} catch {
    exit 1
}
`)
