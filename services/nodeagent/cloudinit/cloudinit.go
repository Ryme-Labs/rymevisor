package cloudinit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type UserData struct {
	Hostname    string
	SSHKeys     []string
	User        string
	Password    string
	Packages    []string
	RunCommands []string
}

type NetworkConfig struct {
	Interfaces []NetworkInterface
}

type NetworkInterface struct {
	Name        string
	Addresses   []string
	Gateway     string
	Nameservers []string
}

func GenerateISO(ctx context.Context, outputDir string, meta, user, network []byte) (string, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	metaPath := filepath.Join(outputDir, "meta-data")
	userPath := filepath.Join(outputDir, "user-data")
	netPath := filepath.Join(outputDir, "network-config")

	if err := os.WriteFile(metaPath, meta, 0644); err != nil {
		return "", fmt.Errorf("write meta-data: %w", err)
	}
	if err := os.WriteFile(userPath, user, 0644); err != nil {
		return "", fmt.Errorf("write user-data: %w", err)
	}
	if err := os.WriteFile(netPath, network, 0644); err != nil {
		return "", fmt.Errorf("write network-config: %w", err)
	}

	isoPath := filepath.Join(outputDir, "cidata.iso")
	cmd := exec.CommandContext(ctx, "genisoimage",
		"-output", isoPath,
		"-v", "-J",
		"-joliet-long",
		"meta-data", "user-data", "network-config",
	)
	cmd.Dir = outputDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("genisoimage: %w: %s", err, string(output))
	}

	return isoPath, nil
}

func GenerateMetaData(hostname string) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "instance-id: %s\n", hostname)
	fmt.Fprintf(&buf, "local-hostname: %s\n", hostname)
	return buf.Bytes()
}

func GenerateNetworkConfig(cfg NetworkConfig) []byte {
	var buf bytes.Buffer

	buf.WriteString("version: 2\n")
	buf.WriteString("ethernets:\n")

	for _, iface := range cfg.Interfaces {
		name := iface.Name
		if name == "" {
			name = "ens3"
		}
		fmt.Fprintf(&buf, "  %s:\n", name)
		fmt.Fprintf(&buf, "    dhcp4: false\n")

		if len(iface.Addresses) > 0 {
			buf.WriteString("    addresses:\n")
			for _, addr := range iface.Addresses {
				fmt.Fprintf(&buf, "      - %s\n", addr)
			}
		}

		if iface.Gateway != "" {
			fmt.Fprintf(&buf, "    gateway4: %s\n", iface.Gateway)
		}

		if len(iface.Nameservers) > 0 {
			buf.WriteString("    nameservers:\n")
			buf.WriteString("      addresses:\n")
			for _, ns := range iface.Nameservers {
				fmt.Fprintf(&buf, "        - %s\n", ns)
			}
		}
	}

	return buf.Bytes()
}

func GenerateUserDataFromString(hostname, sshKey, user, password string) []byte {
	var buf bytes.Buffer
	buf.WriteString("#cloud-config\n")
	if hostname != "" {
		fmt.Fprintf(&buf, "hostname: %s\n", hostname)
	}
	if sshKey != "" {
		sshKeys := strings.Split(sshKey, "\n")
		if len(sshKeys) > 0 {
			buf.WriteString("ssh_authorized_keys:\n")
			for _, key := range sshKeys {
				fmt.Fprintf(&buf, "  - %s\n", key)
			}
		}
	}
	if user == "" {
		user = "ubuntu"
	}
	buf.WriteString("users:\n")
	fmt.Fprintf(&buf, "  - name: %s\n", user)
	fmt.Fprintf(&buf, "    sudo: ['ALL=(ALL) NOPASSWD:ALL']\n")
	fmt.Fprintf(&buf, "    shell: /bin/bash\n")
	fmt.Fprintf(&buf, "    lock_passwd: false\n")
	if password != "" {
		fmt.Fprintf(&buf, "    passwd: %s\n", password)
	}
	buf.WriteString("packages:\n")
	fmt.Fprintf(&buf, "  - %s\n", "qemu-guest-agent")
	buf.WriteString("runcmd:\n")
	fmt.Fprintf(&buf, "  - %s\n", "systemctl enable qemu-guest-agent")
	fmt.Fprintf(&buf, "  - %s\n", "systemctl start qemu-guest-agent")
	buf.WriteString("package_update: true\n")
	buf.WriteString("package_upgrade: true\n")
	return buf.Bytes()
}
