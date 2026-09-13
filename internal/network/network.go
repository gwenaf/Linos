package network

import (
	"net"
	"slices"
)

// LocalIPs lists the private IPv4 addresses phones may use to reach this machine,
// the default route's address first (a Windows hotspot usually adds 192.168.137.1).
func LocalIPs() []string {
	var primary net.IP
	// Dialing UDP sends no packet: it only asks the OS which interface would route outside.
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		primary = conn.LocalAddr().(*net.UDPAddr).IP
		conn.Close()
	}
	addrs, _ := net.InterfaceAddrs() // no interfaces listed: only the primary address is offered
	return candidates(primary, addrs)
}

func candidates(primary net.IP, addrs []net.Addr) []string {
	ips := []string{}
	add := func(ip net.IP) {
		if ip = ip.To4(); ip != nil && ip.IsPrivate() && !slices.Contains(ips, ip.String()) {
			ips = append(ips, ip.String())
		}
	}
	add(primary)
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			add(ipnet.IP)
		}
	}
	return ips
}
