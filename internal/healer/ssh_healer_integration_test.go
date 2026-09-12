package healer

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yudemir1/phoenix/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// exitStatusMsg is the RFC 4254 §6.10 payload carried by an "exit-status"
// channel request.
type exitStatusMsg struct {
	Status uint32
}

// testSSHServer is a minimal, real SSH server used to exercise SSHHealer
// end-to-end without needing an actual remote machine: it accepts one
// "exec" request per session, hands the received command to handle, and
// replies with whatever output/exit status handle returns.
type testSSHServer struct {
	addr    string
	hostKey ssh.PublicKey
}

func startTestSSHServer(t *testing.T, clientPubKey ssh.PublicKey, handle func(cmd string) (output []byte, exitStatus uint32)) *testSSHServer {
	t.Helper()

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("could not generate host key: %v", err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatalf("could not create host signer: %v", err)
	}

	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
			if string(pubKey.Marshal()) == string(clientPubKey.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("unknown public key")
		},
	}
	serverConfig.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed, test is done
			}
			go serveOneSSHConnection(conn, serverConfig, handle)
		}
	}()

	return &testSSHServer{addr: ln.Addr().String(), hostKey: hostSigner.PublicKey()}
}

func serveOneSSHConnection(conn net.Conn, serverConfig *ssh.ServerConfig, handle func(cmd string) ([]byte, uint32)) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, serverConfig)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}

		go func() {
			defer channel.Close()
			for req := range requests {
				if req.Type != "exec" {
					if req.WantReply {
						req.Reply(false, nil)
					}
					continue
				}

				cmd := parseExecCommand(req.Payload)
				req.Reply(true, nil)

				output, status := handle(cmd)
				channel.Write(output)
				channel.SendRequest("exit-status", false, ssh.Marshal(exitStatusMsg{Status: status}))
				return
			}
		}()
	}
}

// parseExecCommand decodes the payload of an "exec" channel request, which
// per RFC 4254 §6.5 is a single SSH string: a 4-byte big-endian length
// prefix followed by that many bytes of command text.
func parseExecCommand(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	n := binary.BigEndian.Uint32(payload[:4])
	if int(4+n) > len(payload) {
		return string(payload[4:])
	}
	return string(payload[4 : 4+n])
}

// generateTestClientKey creates a fresh ed25519 keypair, writes the private
// half to a temp file in the same OpenSSH PEM format SSHHealer expects to
// read from config.SSHConfig.KeyPath, and returns the signer (so tests can
// hand its public key to startTestSSHServer) plus the file path.
func generateTestClientKey(t *testing.T) (ssh.Signer, string) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("could not generate client key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("could not create client signer: %v", err)
	}

	block, err := ssh.MarshalPrivateKey(priv, "phoenix-test-key")
	if err != nil {
		t.Fatalf("could not marshal private key: %v", err)
	}

	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("could not write private key file: %v", err)
	}

	return signer, keyPath
}

func serviceAgainst(t *testing.T, srv *testSSHServer, keyPath string, recover config.Recover) config.ServiceConfig {
	t.Helper()
	host, portStr, err := net.SplitHostPort(srv.addr)
	if err != nil {
		t.Fatalf("could not split test server address %q: %v", srv.addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("could not parse test server port %q: %v", portStr, err)
	}

	return config.ServiceConfig{
		Name: "test-service",
		SSH: config.SSHConfig{
			Host:    host,
			Port:    port,
			User:    "deploy",
			KeyPath: keyPath,
		},
		Recover: recover,
	}
}

func TestSSHHealer_Heal(t *testing.T) {
	t.Run("runs_the_recovery_command_over_a_real_ssh_connection_and_succeeds", func(t *testing.T) {
		clientSigner, keyPath := generateTestClientKey(t)

		var mu sync.Mutex
		var receivedCmd string
		srv := startTestSSHServer(t, clientSigner.PublicKey(), func(cmd string) ([]byte, uint32) {
			mu.Lock()
			receivedCmd = cmd
			mu.Unlock()
			return []byte("container restarted\n"), 0
		})

		svc := serviceAgainst(t, srv, keyPath, config.Recover{Strategy: "docker_restart", Target: "web-api-container"})

		h := healerTrusting(t, srv)
		if err := h.Heal(context.Background(), svc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()
		if want := "docker restart web-api-container"; receivedCmd != want {
			t.Errorf("server received command %q, want %q", receivedCmd, want)
		}
	})

	t.Run("returns_an_error_when_the_remote_command_exits_non_zero", func(t *testing.T) {
		clientSigner, keyPath := generateTestClientKey(t)
		srv := startTestSSHServer(t, clientSigner.PublicKey(), func(cmd string) ([]byte, uint32) {
			return []byte("systemctl: unit not found\n"), 1
		})

		svc := serviceAgainst(t, srv, keyPath, config.Recover{Strategy: "systemd_restart", Target: "worker.service"})

		h := healerTrusting(t, srv)
		err := h.Heal(context.Background(), svc)
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "unit not found") {
			t.Errorf("error = %q, want it to include the remote command's output", err.Error())
		}
	})

	t.Run("returns_an_error_for_an_unknown_recover_strategy_without_ever_connecting", func(t *testing.T) {
		// This path fails before any dial, so host key verification is
		// irrelevant here.
		h := NewInsecureSSHHealer(0)
		svc := config.ServiceConfig{
			Name:    "svc",
			SSH:     config.SSHConfig{Host: "127.0.0.1", Port: 1, User: "deploy", KeyPath: "/nonexistent"},
			Recover: config.Recover{Strategy: "reboot_host"},
		}

		err := h.Heal(context.Background(), svc)
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "Unknown recover strategy") {
			t.Errorf("error = %q, want it to mention the unknown strategy (and never attempt to connect)", err.Error())
		}
	})

	t.Run("returns_an_error_when_the_private_key_file_does_not_exist", func(t *testing.T) {
		// Fails while reading the client key, before any dial.
		h := NewInsecureSSHHealer(0)
		svc := config.ServiceConfig{
			Name:    "svc",
			SSH:     config.SSHConfig{Host: "127.0.0.1", Port: 22, User: "deploy", KeyPath: "/nonexistent/key"},
			Recover: config.Recover{Strategy: "docker_restart", Target: "x"},
		}

		err := h.Heal(context.Background(), svc)
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
	})
}

// writeKnownHosts writes a known_hosts file containing exactly one entry
// mapping addr to key, and returns its path. knownhosts.Normalize takes care
// of the "[host]:port" form that non-22 ports require.
func writeKnownHosts(t *testing.T, addr string, key ssh.PublicKey) string {
	t.Helper()

	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, key)
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("could not write known_hosts: %v", err)
	}
	return path
}

// healerTrusting returns an SSHHealer whose known_hosts trusts exactly srv.
func healerTrusting(t *testing.T, srv *testSSHServer) *SSHHealer {
	t.Helper()

	h, err := NewSSHHealer(writeKnownHosts(t, srv.addr, srv.hostKey), 0)
	if err != nil {
		t.Fatalf("could not create healer: %v", err)
	}
	return h
}

// randomHostKey returns a public key belonging to nobody in this test, used
// to stand in for an impostor host.
func randomHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("could not generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("could not create signer: %v", err)
	}
	return signer.PublicKey()
}

func TestSSHHealer_HostKeyVerification(t *testing.T) {
	t.Run("refuses_to_connect_when_the_host_key_does_not_match_the_recorded_one", func(t *testing.T) {
		clientSigner, keyPath := generateTestClientKey(t)
		var reached bool
		srv := startTestSSHServer(t, clientSigner.PublicKey(), func(cmd string) ([]byte, uint32) {
			reached = true
			return nil, 0
		})

		// known_hosts records this address, but with somebody else's key:
		// exactly what a man-in-the-middle looks like.
		kh := writeKnownHosts(t, srv.addr, randomHostKey(t))
		h, err := NewSSHHealer(kh, 0)
		if err != nil {
			t.Fatalf("could not create healer: %v", err)
		}

		svc := serviceAgainst(t, srv, keyPath, config.Recover{Strategy: "docker_restart", Target: "c1"})
		err = h.Heal(context.Background(), svc)

		if err == nil {
			t.Fatal("expected the connection to be refused, got nil")
		}
		if !strings.Contains(err.Error(), "key mismatch") && !strings.Contains(err.Error(), "knownhosts") {
			t.Errorf("error = %q, want a host key verification failure", err.Error())
		}
		if reached {
			t.Error("the recovery command reached the impostor host; it must never be sent")
		}
	})

	t.Run("refuses_to_connect_to_a_host_that_is_not_listed_in_known_hosts", func(t *testing.T) {
		clientSigner, keyPath := generateTestClientKey(t)
		var reached bool
		srv := startTestSSHServer(t, clientSigner.PublicKey(), func(cmd string) ([]byte, uint32) {
			reached = true
			return nil, 0
		})

		// A well-formed known_hosts that simply says nothing about this host.
		kh := writeKnownHosts(t, "198.51.100.7:22", randomHostKey(t))
		h, err := NewSSHHealer(kh, 0)
		if err != nil {
			t.Fatalf("could not create healer: %v", err)
		}

		svc := serviceAgainst(t, srv, keyPath, config.Recover{Strategy: "docker_restart", Target: "c1"})
		if err := h.Heal(context.Background(), svc); err == nil {
			t.Fatal("expected the connection to be refused for an unknown host, got nil")
		}
		if reached {
			t.Error("the recovery command reached an unverified host; it must never be sent")
		}
	})

	t.Run("a_matching_known_hosts_entry_allows_the_recovery_to_run", func(t *testing.T) {
		clientSigner, keyPath := generateTestClientKey(t)
		var mu sync.Mutex
		var receivedCmd string
		srv := startTestSSHServer(t, clientSigner.PublicKey(), func(cmd string) ([]byte, uint32) {
			mu.Lock()
			receivedCmd = cmd
			mu.Unlock()
			return []byte("ok\n"), 0
		})

		h := healerTrusting(t, srv)
		svc := serviceAgainst(t, srv, keyPath, config.Recover{Strategy: "docker_restart", Target: "c1"})

		if err := h.Heal(context.Background(), svc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()
		if want := "docker restart c1"; receivedCmd != want {
			t.Errorf("server received %q, want %q", receivedCmd, want)
		}
	})

	t.Run("a_missing_known_hosts_file_fails_at_construction_not_at_recovery_time", func(t *testing.T) {
		h, err := NewSSHHealer(filepath.Join(t.TempDir(), "does-not-exist"), 0)

		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if h != nil {
			t.Errorf("healer should be nil on error, got %+v", h)
		}
		if !strings.Contains(err.Error(), "known_hosts") {
			t.Errorf("error = %q, want it to name known_hosts so the operator knows what to fix", err.Error())
		}
	})

	t.Run("a_malformed_known_hosts_file_fails_at_construction", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "known_hosts")
		if err := os.WriteFile(path, []byte("this is not a known_hosts line\n"), 0o600); err != nil {
			t.Fatalf("could not write file: %v", err)
		}

		if _, err := NewSSHHealer(path, 0); err == nil {
			t.Error("expected a malformed known_hosts file to be rejected, got nil")
		}
	})

	t.Run("the_insecure_healer_still_connects_without_any_known_hosts", func(t *testing.T) {
		clientSigner, keyPath := generateTestClientKey(t)
		srv := startTestSSHServer(t, clientSigner.PublicKey(), func(cmd string) ([]byte, uint32) {
			return []byte("ok\n"), 0
		})

		h := NewInsecureSSHHealer(0)
		svc := serviceAgainst(t, srv, keyPath, config.Recover{Strategy: "docker_restart", Target: "c1"})

		if err := h.Heal(context.Background(), svc); err != nil {
			t.Errorf("the insecure healer should skip verification entirely, got: %v", err)
		}
	})
}

func TestSSHHealer_Timeout(t *testing.T) {
	t.Run("the_configured_ssh_timeout_is_used_instead_of_the_built_in_default", func(t *testing.T) {
		h, err := NewSSHHealer(writeKnownHosts(t, "127.0.0.1:22", randomHostKey(t)), 42*time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := 42 * time.Second; h.timeout != want {
			t.Errorf("timeout = %s, want %s (is the config value actually wired through?)", h.timeout, want)
		}
	})

	t.Run("a_zero_timeout_falls_back_to_the_built_in_default", func(t *testing.T) {
		h := NewInsecureSSHHealer(0)
		if h.timeout != defaultSSHTimeout {
			t.Errorf("timeout = %s, want the %s default", h.timeout, defaultSSHTimeout)
		}
	})

	t.Run("the_timeout_actually_reaches_the_ssh_client_config", func(t *testing.T) {
		// A host that accepts the TCP connection but never speaks SSH makes
		// the handshake hang; the client-side timeout is the only thing that
		// ends it, so the call returning at all proves the value is used.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("could not listen: %v", err)
		}
		defer ln.Close()
		go func() {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			select {} // never respond
		}()

		_, keyPath := generateTestClientKey(t)
		h := NewInsecureSSHHealer(300 * time.Millisecond)
		host, portStr, _ := net.SplitHostPort(ln.Addr().String())
		port, _ := strconv.Atoi(portStr)

		svc := config.ServiceConfig{
			Name:    "silent-host",
			SSH:     config.SSHConfig{Host: host, Port: port, User: "deploy", KeyPath: keyPath},
			Recover: config.Recover{Strategy: "docker_restart", Target: "c1"},
		}

		// Run in a goroutine with an outer bound: if the timeout does not
		// work this test must fail, not hang forever.
		done := make(chan error, 1)
		start := time.Now()
		go func() { done <- h.Heal(context.Background(), svc) }()

		select {
		case err := <-done:
			if err == nil {
				t.Fatal("expected the handshake to time out, got nil")
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("took %s: the configured 300ms timeout is not in effect", elapsed)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Heal never returned: a host that accepts TCP but never speaks SSH hangs the runner goroutine forever")
		}
	})
}
