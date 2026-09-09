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

	"github.com/yudemir1/phoenix/internal/config"
	"golang.org/x/crypto/ssh"
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
	addr string
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

	return &testSSHServer{addr: ln.Addr().String()}
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

		h := NewSSHHealer()
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

		h := NewSSHHealer()
		err := h.Heal(context.Background(), svc)
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "unit not found") {
			t.Errorf("error = %q, want it to include the remote command's output", err.Error())
		}
	})

	t.Run("returns_an_error_for_an_unknown_recover_strategy_without_ever_connecting", func(t *testing.T) {
		h := NewSSHHealer()
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
		h := NewSSHHealer()
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
