package metrics

import (
	"bufio"
	"encoding/hex"
	"io"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
)

type ListeningSocket struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     uint16 `json:"port"`
	Scope    string `json:"scope"`
}

type procNetFile struct {
	path       string
	protocol   string
	addressLen int
	state      string
}

func getListeningSockets() []ListeningSocket {
	files := []procNetFile{
		{path: "/proc/net/tcp", protocol: "tcp", addressLen: 4, state: "0A"},
		{path: "/proc/net/tcp6", protocol: "tcp6", addressLen: 16, state: "0A"},
		{path: "/proc/net/udp", protocol: "udp", addressLen: 4, state: "07"},
		{path: "/proc/net/udp6", protocol: "udp6", addressLen: 16, state: "07"},
	}

	var sockets []ListeningSocket
	for _, file := range files {
		f, err := os.Open(file.path)
		if err != nil {
			continue
		}
		sockets = append(sockets, parseProcNet(f, file.protocol, file.addressLen, file.state)...)
		f.Close()
	}

	sort.Slice(sockets, func(i, j int) bool {
		left, right := sockets[i], sockets[j]
		if scopePriority(left.Scope) != scopePriority(right.Scope) {
			return scopePriority(left.Scope) < scopePriority(right.Scope)
		}
		if left.Port != right.Port {
			return left.Port < right.Port
		}
		if left.Protocol != right.Protocol {
			return left.Protocol < right.Protocol
		}
		return left.Address < right.Address
	})

	return sockets
}

func parseProcNet(r io.Reader, protocol string, addressLen int, listeningState string) []ListeningSocket {
	var sockets []ListeningSocket
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || fields[3] != listeningState {
			continue
		}

		address, port, ok := parseProcNetAddress(fields[1], addressLen)
		if !ok {
			continue
		}

		sockets = append(sockets, ListeningSocket{
			Protocol: protocol,
			Address:  address.String(),
			Port:     port,
			Scope:    socketScope(address),
		})
	}
	return sockets
}

func parseProcNetAddress(value string, addressLen int) (netip.Addr, uint16, bool) {
	addressHex, portHex, ok := strings.Cut(value, ":")
	if !ok || len(addressHex) != addressLen*2 {
		return netip.Addr{}, 0, false
	}

	addressBytes, err := hex.DecodeString(addressHex)
	if err != nil {
		return netip.Addr{}, 0, false
	}
	portValue, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return netip.Addr{}, 0, false
	}

	// Linux writes addresses in host-endian 32-bit words in /proc/net files.
	for start := 0; start < len(addressBytes); start += 4 {
		for left, right := start, start+3; left < right; left, right = left+1, right-1 {
			addressBytes[left], addressBytes[right] = addressBytes[right], addressBytes[left]
		}
	}

	var address netip.Addr
	if addressLen == 4 {
		address = netip.AddrFrom4([4]byte(addressBytes))
	} else if addressLen == 16 {
		address = netip.AddrFrom16([16]byte(addressBytes))
	} else {
		return netip.Addr{}, 0, false
	}
	return address, uint16(portValue), true
}

func socketScope(address netip.Addr) string {
	if address.IsUnspecified() {
		return "all interfaces"
	}
	if address.IsLoopback() {
		return "loopback"
	}
	return "interface"
}

func scopePriority(scope string) int {
	switch scope {
	case "all interfaces":
		return 0
	case "interface":
		return 1
	default:
		return 2
	}
}
