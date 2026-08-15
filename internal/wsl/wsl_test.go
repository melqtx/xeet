package wsl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

type runCall struct {
	name  string
	args  []string
	stdin []byte
}

type fakeRunner struct {
	calls []runCall
	run   func(runCall) ([]byte, error)
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	call := runCall{name: name, args: slices.Clone(args), stdin: slices.Clone(stdin)}
	f.calls = append(f.calls, call)
	return f.run(call)
}

func activeBridge(runner commandRunner) bridge {
	return bridge{
		goos:      "linux",
		lookupEnv: func(key string) (string, bool) { return "Ubuntu", key == "WSL_DISTRO_NAME" },
		stat:      func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		lookPath: func(name string) (string, error) {
			return "/interop/" + name, nil
		},
		runner: runner,
	}
}

func TestActive(t *testing.T) {
	tests := []struct {
		name string
		goos string
		env  bool
		stat error
		want bool
	}{
		{"macOS ignores WSL variables", "darwin", true, nil, false},
		{"environment", "linux", true, os.ErrNotExist, true},
		{"interop marker", "linux", false, nil, true},
		{"ordinary Linux", "linux", false, os.ErrNotExist, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := bridge{
				goos: tt.goos,
				lookupEnv: func(string) (string, bool) {
					return "", tt.env
				},
				stat: func(string) (os.FileInfo, error) {
					return nil, tt.stat
				},
			}
			if got := b.active(); got != tt.want {
				t.Fatalf("active() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAppDataUsesWindowsAndWSLPathTools(t *testing.T) {
	runner := &fakeRunner{}
	runner.run = func(call runCall) ([]byte, error) {
		switch call.name {
		case "/interop/powershell.exe":
			if !slices.Equal(call.args, []string{
				"-NoLogo", "-NoProfile", "-NonInteractive",
				"-EncodedCommand", encodedAppDataScript,
			}) {
				t.Fatalf("PowerShell args = %#v", call.args)
			}
			path := []byte("C:\\Users\\Zoë Lovelace\\AppData\\Roaming")
			return []byte(base64.StdEncoding.EncodeToString(path)), nil
		case "/interop/wslpath":
			if !slices.Equal(call.args, []string{"-u", `C:\Users\Zoë Lovelace\AppData\Roaming`}) {
				t.Fatalf("wslpath args = %#v", call.args)
			}
			return []byte("/windows/Users/Zoë Lovelace/AppData/Roaming\n"), nil
		default:
			t.Fatalf("unexpected command %q", call.name)
			return nil, nil
		}
	}
	got, err := activeBridge(runner).appData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "/windows/Users/Zoë Lovelace/AppData/Roaming" {
		t.Fatalf("AppData = %q", got)
	}
}

func TestAppDataFailsClosed(t *testing.T) {
	runner := &fakeRunner{run: func(runCall) ([]byte, error) {
		return []byte("not base64"), nil
	}}
	_, err := activeBridge(runner).appData(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestDPAPISecretOnlyTravelsOnStdin(t *testing.T) {
	const secret = "session-material-must-not-leak"
	runner := &fakeRunner{}
	runner.run = func(call runCall) ([]byte, error) {
		for _, arg := range call.args {
			if strings.Contains(arg, secret) {
				t.Fatalf("secret leaked into argv: %#v", call.args)
			}
		}
		var request dpapiRequest
		if err := json.Unmarshal(call.stdin, &request); err != nil {
			t.Fatal(err)
		}
		decoded, err := base64.StdEncoding.DecodeString(request.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if string(decoded) != secret || request.Operation != "protect" || request.Key != "auth_token" {
			t.Fatalf("request = %+v, payload %q", request, decoded)
		}
		return []byte(base64.StdEncoding.EncodeToString([]byte("ciphertext"))), nil
	}

	got, err := activeBridge(runner).dpapi(context.Background(), "protect", "auth_token", []byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ciphertext" {
		t.Fatalf("result = %q", got)
	}
}

func TestDPAPIErrorsDoNotExposeProcessMaterial(t *testing.T) {
	const secret = "private-cookie"
	runner := &fakeRunner{run: func(runCall) ([]byte, error) {
		return []byte(secret), errors.New(secret)
	}}
	_, err := activeBridge(runner).dpapi(context.Background(), "protect", "ct0", []byte(secret))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked secret: %v", err)
	}
}

func TestDPAPIRejectsInvalidOutputAndKeys(t *testing.T) {
	runner := &fakeRunner{run: func(runCall) ([]byte, error) {
		return []byte("not base64"), nil
	}}
	b := activeBridge(runner)
	if _, err := b.dpapi(context.Background(), "protect", "../auth", []byte("x")); err == nil {
		t.Fatal("invalid key accepted")
	}
	if _, err := b.dpapi(context.Background(), "protect", "auth_token", []byte("x")); err == nil {
		t.Fatal("invalid PowerShell output accepted")
	}
}

func TestDPAPIScriptLoadsProtectedDataAssembly(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(encodedDPAPIScript)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("encoded PowerShell byte length = %d, want an even number", len(raw))
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	script := string(utf16.Decode(units))
	if !strings.Contains(script, "Add-Type -AssemblyName System.Security") {
		t.Fatal("DPAPI script does not load System.Security for Windows PowerShell 5.1")
	}
}
