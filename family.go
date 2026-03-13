package main

import (
	"net"

	"github.com/miekg/dns"
)

func (family ipFamily) scanFamilies() []ipFamily {
	switch family {
	case family4:
		return []ipFamily{family4}
	case familyAll:
		return []ipFamily{family6, family4}
	default:
		return []ipFamily{family6}
	}
}

func (family ipFamily) lookupNetwork() string {
	switch family {
	case family4:
		return "ip4"
	default:
		return "ip6"
	}
}

func (family ipFamily) dnsType() uint16 {
	switch family {
	case family4:
		return dns.TypeA
	default:
		return dns.TypeAAAA
	}
}

func (family ipFamily) traceFlag() string {
	switch family {
	case family4:
		return "-4"
	default:
		return "-6"
	}
}

func familyFromIP(ip net.IP) ipFamily {
	if ip == nil {
		return ""
	}
	if ip.To4() != nil {
		return family4
	}
	if ip.To16() != nil {
		return family6
	}
	return ""
}

func familyOrder(family ipFamily) int {
	switch family {
	case family6:
		return 0
	case family4:
		return 1
	default:
		return 2
	}
}

func missingStatus(family ipFamily) string {
	switch family {
	case family4:
		return "no_a"
	case family6:
		return "no_aaaa"
	default:
		return "no_records"
	}
}
