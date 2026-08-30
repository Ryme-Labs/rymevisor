package ipam

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
)




func NextAvailableIP(cidr string, usedIPs []string) (allocated string, gateway string, err error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}

	ip4 := ipNet.IP.To4()
	if ip4 == nil {
		return "", "", fmt.Errorf("only IPv4 CIDR supported, got %q", cidr)
	}

	mask := binary.BigEndian.Uint32(ipNet.Mask)
	network := binary.BigEndian.Uint32(ip4)
	broadcast := network | ^mask


	gwInt := network + 1
	gwIP := make(net.IP, 4)
	binary.BigEndian.PutUint32(gwIP, gwInt)
	gateway = gwIP.String()

	usedSet := make(map[uint32]bool, len(usedIPs))
	for _, u := range usedIPs {

		ipStr := u
		if parsed, _, err := net.ParseCIDR(u); err == nil {
			ipStr = parsed.String()
		}
		if ip := net.ParseIP(ipStr); ip != nil {
			if v4 := ip.To4(); v4 != nil {
				usedSet[binary.BigEndian.Uint32(v4)] = true
			}
		}
	}


	usedSet[network] = true
	usedSet[gwInt] = true
	usedSet[broadcast] = true



	for candidate := network + 2; candidate < broadcast; candidate++ {
		if usedSet[candidate] {
			continue
		}
		candidateIP := make(net.IP, 4)
		binary.BigEndian.PutUint32(candidateIP, candidate)
		if !ipNet.Contains(candidateIP) {
			continue
		}

		ones, _ := ipNet.Mask.Size()
		allocated = fmt.Sprintf("%s/%d", candidateIP.String(), ones)
		return allocated, gateway, nil
	}

	return "", gateway, fmt.Errorf("no available IPs in %s (used %d)", cidr, len(usedSet))
}


func GatewayIP(cidr string) (string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", err
	}
	ip4 := ipNet.IP.To4()
	if ip4 == nil {
		return "", fmt.Errorf("only IPv4")
	}
	network := binary.BigEndian.Uint32(ip4)
	gw := network + 1
	gwIP := make(net.IP, 4)
	binary.BigEndian.PutUint32(gwIP, gw)
	return gwIP.String(), nil
}


func BareIP(cidrOrIP string) string {
	if ip, _, err := net.ParseCIDR(cidrOrIP); err == nil {
		return ip.String()
	}
	if ip := net.ParseIP(cidrOrIP); ip != nil {
		return ip.String()
	}
	return cidrOrIP
}


func CIDRPrefix(cidr string) int {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return 24
	}
	ones, _ := ipNet.Mask.Size()
	return ones
}

func BridgeForNetwork(networkID string) (string, error) {
	if networkID == "" {
		return "", fmt.Errorf("networkID is required")
	}
	cleaned := strings.ReplaceAll(networkID, "-", "")
	if len(cleaned) < 8 {
		return "br-" + cleaned, nil
	}
	return "br-" + cleaned[:8], nil
}
