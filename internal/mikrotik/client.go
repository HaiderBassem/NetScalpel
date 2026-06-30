package mikrotik

import (
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// ConnectionResult holds the outcome of waiting for a PPPoE link to come up.
type ConnectionResult struct {
	Connected         bool
	AssignedIP        string
	TimeToConnectSecs float64
	Error             string
}

// Client wraps an SSH session to a MikroTik RouterOS device.
type Client struct {
	address  string
	username string
	password string
	timeout  time.Duration
	conn     *ssh.Client
}

// New creates a new Client. Call Connect() before using it.
func New(address string, port int, username, password string, connectTimeout time.Duration) *Client {
	return &Client{
		address:  fmt.Sprintf("%s:%d", address, port),
		username: username,
		password: password,
		timeout:  connectTimeout,
	}
}

// Connect establishes the SSH connection to the MikroTik router.
// It deliberately enables legacy ciphers/key-exchange algorithms required by
// older RouterOS versions (6.x).
func (c *Client) Connect() error {
	sshCfg := &ssh.ClientConfig{
		User: c.username,
		Auth: []ssh.AuthMethod{
			ssh.Password(c.password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) (answers []string, err error) {
				answers = make([]string, len(questions))
				for i := range answers {
					answers[i] = c.password
				}
				return answers, nil
			}),
		},
		// RouterOS 6.x uses older host-key algorithms; accept any to keep
		// compatibility. This is intentional for a controlled lab environment.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
		HostKeyAlgorithms: []string{
			"ssh-rsa",
			"ssh-dss",
			"ecdsa-sha2-nistp256",
			"ssh-ed25519",
		},
		Timeout:         c.timeout,
		// Extend the cipher/kex lists for RouterOS 6 compatibility.
		Config: ssh.Config{
			KeyExchanges: []string{
				"diffie-hellman-group14-sha1",
				"diffie-hellman-group1-sha1",
				"diffie-hellman-group14-sha256",
				"ecdh-sha2-nistp256",
				"ecdh-sha2-nistp384",
				"ecdh-sha2-nistp521",
				"curve25519-sha256",
				"curve25519-sha256@libssh.org",
			},
			Ciphers: []string{
				"aes128-cbc",
				"aes128-ctr",
				"aes192-ctr",
				"aes256-ctr",
				"aes128-gcm@openssh.com",
				"chacha20-poly1305@openssh.com",
				"3des-cbc",
			},
			MACs: []string{
				"hmac-sha1",
				"hmac-sha2-256",
				"hmac-sha2-512",
				"hmac-sha1-96",
			},
		},
	}

	conn, err := ssh.Dial("tcp", c.address, sshCfg)
	if err != nil {
		return fmt.Errorf("SSH dial to %s failed: %w", c.address, err)
	}
	c.conn = conn
	return nil
}

// Close tears down the SSH connection.
func (c *Client) Close() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

// Run executes a single RouterOS command and returns the raw output.
// Each call opens a new session (RouterOS SSH does not support multiplexing).
func (c *Client) Run(cmd string) (string, error) {
	if c.conn == nil {
		return "", fmt.Errorf("not connected — call Connect() first")
	}

	session, err := c.conn.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	out, err := session.CombinedOutput(cmd)
	output := strings.TrimSpace(string(out))
	if err != nil {
		// Attach the device output to the error so callers see the full picture.
		if output != "" {
			return output, fmt.Errorf("command %q failed: %w — device says: %s", cmd, err, output)
		}
		return "", fmt.Errorf("command %q failed: %w", cmd, err)
	}
	return output, nil
}

// DisableAll disables every PPPoE client interface in the provided list.
// It disables them one by one and collects all errors so a single failure does
// not abort the rest.
func (c *Client) DisableAll(interfaceNames []string) error {
	var errs []string
	for _, name := range interfaceNames {
		cmd := fmt.Sprintf(`/interface pppoe-client disable "%s"`, name)
		if _, err := c.Run(cmd); err != nil {
			errs = append(errs, fmt.Sprintf("disable %q: %v", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("DisableAll encountered %d error(s): %s",
			len(errs), strings.Join(errs, "; "))
	}
	return nil
}

// EnablePackage enables a single PPPoE client interface by name.
func (c *Client) EnablePackage(name string) error {
	cmd := fmt.Sprintf(`/interface pppoe-client enable "%s"`, name)
	_, err := c.Run(cmd)
	if err != nil {
		return fmt.Errorf("enable %q: %w", name, err)
	}
	return nil
}

// DisablePackage disables a single PPPoE client interface by name.
func (c *Client) DisablePackage(name string) error {
	cmd := fmt.Sprintf(`/interface pppoe-client disable "%s"`, name)
	_, err := c.Run(cmd)
	if err != nil {
		return fmt.Errorf("disable %q: %w", name, err)
	}
	return nil
}

// WaitForConnection polls the PPPoE interface status every 5 seconds until it
// reaches "connected" state, or until the deadline is exceeded.
// It then attempts a single ping to 8.8.8.8 to confirm end-to-end connectivity.
func (c *Client) WaitForConnection(interfaceName string, timeout time.Duration) ConnectionResult {
	const pollInterval = 5 * time.Second
	deadline := time.Now().Add(timeout)
	start := time.Now()

	fmt.Printf("[MIKROTIK] Waiting for %q to connect (timeout: %s)...\n",
		interfaceName, timeout)

	for time.Now().Before(deadline) {
		status, assignedIP, err := c.queryInterfaceStatus(interfaceName)
		if err != nil {
			fmt.Printf("[MIKROTIK] Status query error: %v — retrying...\n", err)
			time.Sleep(pollInterval)
			continue
		}

		fmt.Printf("[MIKROTIK] [%s] status=%s assigned_ip=%s\n",
			interfaceName, status, orDash(assignedIP))

		if strings.EqualFold(status, "connected") {
			// Extra sanity-check: ping 8.8.8.8 from the router itself.
			pingErr := c.pingCheck("8.8.8.8", 3)
			if pingErr != nil {
				fmt.Printf("[MIKROTIK] PPPoE connected but ping failed: %v\n", pingErr)
				return ConnectionResult{
					Connected:         false,
					AssignedIP:        assignedIP,
					TimeToConnectSecs: time.Since(start).Seconds(),
					Error: fmt.Sprintf("PPPoE status=connected but ping to 8.8.8.8 failed: %v",
						pingErr),
				}
			}
			return ConnectionResult{
				Connected:         true,
				AssignedIP:        assignedIP,
				TimeToConnectSecs: time.Since(start).Seconds(),
			}
		}

		time.Sleep(pollInterval)
	}

	// Timeout — collect the last log entry for this interface to explain why.
	reason := c.lastLogReason(interfaceName)
	return ConnectionResult{
		Connected:         false,
		TimeToConnectSecs: time.Since(start).Seconds(),
		Error: fmt.Sprintf("PPPoE timeout after %.0fs waiting for %q to connect — last log: %s",
			timeout.Seconds(), interfaceName, reason),
	}
}

// queryInterfaceStatus runs a monitor command and extracts status + remote-address.
// RouterOS 6 /interface pppoe-client monitor returns key=value lines.
func (c *Client) queryInterfaceStatus(name string) (status, remoteIP string, err error) {
	cmd := fmt.Sprintf(`/interface pppoe-client monitor "%s" once`, name)
	out, err := c.Run(cmd)
	if err != nil {
		// Some RouterOS versions return exit-code 1 even on success; if the
		// output contains "status" we still try to parse it.
		if !strings.Contains(out, "status") {
			return "", "", fmt.Errorf("monitor command failed: %w", err)
		}
	}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "status:"); ok {
			status = strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, "remote-address:"); ok {
			remoteIP = strings.TrimSpace(after)
		}
	}
	return status, remoteIP, nil
}

// pingCheck sends count ICMP pings to the target from the router.
// Returns an error if any ping fails or the command itself errors.
func (c *Client) pingCheck(target string, count int) error {
	cmd := fmt.Sprintf("/ping %s count=%d", target, count)
	out, err := c.Run(cmd)
	if err != nil {
		return fmt.Errorf("ping command failed: %w", err)
	}
	// RouterOS ping output contains "packet-loss=0%" on success.
	// On total failure it shows "100%".
	if strings.Contains(out, "100%") {
		return fmt.Errorf("100%% packet loss pinging %s", target)
	}
	return nil
}

// lastLogReason queries the router log and returns the most recent line that
// mentions the interface, which usually explains why a PPPoE session failed.
func (c *Client) lastLogReason(interfaceName string) string {
	out, err := c.Run("/log print")
	if err != nil {
		return fmt.Sprintf("(could not read log: %v)", err)
	}

	// Walk the log lines in reverse — most recent first.
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.Contains(strings.ToLower(line), strings.ToLower(interfaceName)) {
			return line
		}
	}
	return "(no relevant log entry found)"
}

// GetRouterIdentity returns the /system identity name of the router.
func (c *Client) GetRouterIdentity() (string, error) {
	out, err := c.Run("/system identity print")
	if err != nil {
		return "", fmt.Errorf("could not read router identity: %w", err)
	}
	// Output is typically: "name: KUT-CHR-Test"
	for _, line := range strings.Split(out, "\n") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "name:"); ok {
			return strings.TrimSpace(after), nil
		}
	}
	return strings.TrimSpace(out), nil
}

// IsConnected returns true if the SSH connection is alive.
func (c *Client) IsConnected() bool {
	if c.conn == nil {
		return false
	}
	// Send a keepalive: attempt to open and immediately close a session.
	session, err := c.conn.NewSession()
	if err != nil {
		return false
	}
	_ = session.Close()
	return true
}

// WaitForTCPPort blocks until address:port is reachable or timeout elapses.
// Useful to wait for the router SSH port after a reboot.
// Uses net.JoinHostPort to correctly handle both IPv4 and IPv6 addresses.
func WaitForTCPPort(address string, port int, timeout time.Duration) error {
	target := net.JoinHostPort(address, fmt.Sprintf("%d", port))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", target, 3*time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("TCP port %s not reachable after %s", target, timeout)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
