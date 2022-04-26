package main

import (
	"log"
	"net"
)

func syncNodeIPs(podCIDRs []string, iface *net.Interface) {
	cidrs := make([]*net.IPNet, len(podCIDRs))

	for i, cidrString := range podCIDRs {
		var err error
		_, cidrs[i], err = net.ParseCIDR(cidrString)

		if err != nil {
			log.Print("sync IPs failed: bad CIDR ", cidrString, ": ", err)
			return
		}
	}

	for _, cidr := range cidrs {
		addrs, err := iface.Addrs()
		if err != nil {
			log.Fatal("failed to get link addresses: ", err)
		}

		found := false
		for _, addr := range addrs {
			ip, _, _ := net.ParseCIDR(addr.String())
			if cidr.Contains(ip) {
				found = true
				break
			}
		}

		if !found {
			ip := cidr.IP
			ip[len(cidr.IP)-1]++

			ipMask := ip.String() + "/32"
			if ip.To4() == nil {
				ipMask = ip.String() + "/128"
			}

			log.Print("==> adding IP ", ipMask)
			run("ip", "addr", "add", ipMask, "dev", *ifName)
		}
	}
}
