package healer

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/yudemir1/phoenix/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SShHealer connects to a service's host over SSH and runs recovery commands
type SSHHealer struct {
	dialContext     func(ctx context.Context, network, addr string) (net.Conn, error)
	hostKeyCallback ssh.HostKeyCallback
}

func NewSSHHealer(knownHostsPath string) (*SSHHealer, error) {
	cb, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("Error: could not load known_hosts %s: %w", knownHostsPath, err)
	}

	var d net.Dialer
	return &SSHHealer{
		dialContext:     d.DialContext,
		hostKeyCallback: cb,
	}, nil
}

func NewInsecureSSHHealer() *SSHHealer {
	var d net.Dialer
	return &SSHHealer{
		dialContext:     d.DialContext,
		hostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
}

func (h *SSHHealer) Heal(ctx context.Context, s config.ServiceConfig) error {
	cmd, err := buildRecoveryCommand(s.Recover)
	if err != nil {
		return err
	}
	client, err := h.connect(ctx, s.SSH)
	if err != nil {
		return fmt.Errorf("Error: could not connect to %s over SSH: %w", s.SSH.Host, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("Error: could not open SSH session to %s: %w", s.SSH.Host, err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return fmt.Errorf("Error: recovery command %q failed on %s: %w (output: %s)", cmd, s.SSH.Host, err, string(output))
	}
	return nil
}

func (h *SSHHealer) connect(ctx context.Context, sshCfg config.SSHConfig) (*ssh.Client, error) {
	keyBytes, err := os.ReadFile(sshCfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("Error: could not read private key %s: %w", sshCfg.KeyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("Error: could not parse private key %s: %w", sshCfg.KeyPath, err)
	}

	clientConfig := &ssh.ClientConfig{
		User:            sshCfg.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: h.hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", sshCfg.Host, sshCfg.Port)
	conn, err := h.dialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("Error: could not open tcp connection to %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, clientConfig)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("Error: could not establish ssh handshake with %s: %w", addr, err)
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}
