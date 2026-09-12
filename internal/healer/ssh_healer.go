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
	timeout         time.Duration
}

// defaultSSHTimeout is used when the caller passes a non-positive timeout.
const defaultSSHTimeout = 10 * time.Second

func NewSSHHealer(knownHostsPath string, timeout time.Duration) (*SSHHealer, error) {
	cb, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("Error: could not load known_hosts %s: %w", knownHostsPath, err)
	}

	var d net.Dialer
	return &SSHHealer{
		dialContext:     d.DialContext,
		hostKeyCallback: cb,
		timeout:         orDefaultTimeout(timeout),
	}, nil
}

func NewInsecureSSHHealer(timeout time.Duration) *SSHHealer {
	var d net.Dialer
	return &SSHHealer{
		dialContext:     d.DialContext,
		hostKeyCallback: ssh.InsecureIgnoreHostKey(),
		timeout:         orDefaultTimeout(timeout),
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
		Timeout:         h.timeout,
	}

	addr := fmt.Sprintf("%s:%d", sshCfg.Host, sshCfg.Port)

	dialCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	conn, err := h.dialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("Error: could not open tcp connection to %s: %w", addr, err)
	}

	// ssh.ClientConfig.Timeout only bounds the dial that ssh.Dial performs on
	// our behalf. We dial ourselves, so without an explicit deadline the
	// handshake below runs unbounded and a host that accepts TCP but never
	// speaks SSH would hang this goroutine forever.
	if err := conn.SetDeadline(time.Now().Add(h.timeout)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("Error: could not set handshake deadline for %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, clientConfig)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("Error: could not establish ssh handshake with %s: %w", addr, err)
	}

	// Clear the deadline: it must bound the handshake only, never the
	// recovery command that runs afterwards.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		sshConn.Close()
		return nil, fmt.Errorf("Error: could not clear handshake deadline for %s: %w", addr, err)
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}

// orDefaultTimeout keeps a zero or negative configured timeout from turning
// into "no timeout at all" on the SSH client.
func orDefaultTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultSSHTimeout
	}
	return d
}
