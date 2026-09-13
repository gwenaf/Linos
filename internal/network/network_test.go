package network

import (
	"net"
	"reflect"
	"testing"
)

func TestCandidates(t *testing.T) {
	cidr := func(s string) net.Addr {
		ip, ipnet, _ := net.ParseCIDR(s)
		ipnet.IP = ip
		return ipnet
	}
	addrs := []net.Addr{
		cidr("127.0.0.1/8"),                    // loopback
		cidr("192.168.1.10/24"),                // same as primary
		cidr("192.168.137.1/24"),               // Windows hotspot
		cidr("fe80::1/64"),                     // IPv6
		cidr("8.8.4.4/32"),                     // public
		&net.IPAddr{IP: net.IPv4(10, 0, 0, 2)}, // not an IPNet
	}
	got := candidates(net.ParseIP("192.168.1.10"), addrs)
	want := []string{"192.168.1.10", "192.168.137.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	if got := candidates(nil, nil); len(got) != 0 {
		t.Fatalf("candidates without network = %v, want none", got)
	}
}

func TestLocalIPs(t *testing.T) {
	for _, ip := range LocalIPs() {
		if parsed := net.ParseIP(ip); parsed == nil || !parsed.IsPrivate() {
			t.Fatalf("LocalIPs returned %q, want private IPv4 addresses only", ip)
		}
	}
}
