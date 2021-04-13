package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net"
	"strings"

	v1 "k8s.io/api/core/v1"
)

var (
	cniPath = flag.String("cni-config", "/etc/cni/net.d/10-knet.json", "CNI configuration to write")
	cniDNS  = flag.String("cni-dns", "8.8.8.8", "CNI nameservers")
)

func writeCNIConfig(node *v1.Node, iface *net.Interface) {
	if *cniPath == "" {
		return
	}

	ranges := make([][]map[string]interface{}, 0, len(node.Spec.PodCIDRs))
	for _, cidr := range node.Spec.PodCIDRs {
		ranges = append(ranges, []map[string]interface{}{
			{"subnet": cidr},
		})
	}

	ba, err := json.MarshalIndent(map[string]interface{}{
		"cniVersion": "0.3.1",
		"name":       "knet-wg",
		"type":       "ptp",
		"ipam": map[string]interface{}{
			"type":   "host-local",
			"ranges": ranges,
			"routes": []map[string]interface{}{
				{"dst": "0.0.0.0/0"},
			},
		},
		"dns": map[string]interface{}{
			"nameservers": strings.Split(*cniDNS, ","),
		},
		"mtu": iface.MTU,
	}, "", "  ")

	if err != nil {
		log.Fatal("failed to marshal CNI config: ", err)
	}

	log.Print("writing CNI config to ", *cniPath)
	err = ioutil.WriteFile(*cniPath, ba, 0644)
	if err != nil {
		log.Fatal("failed to write CNI config: ", err)
	}
}
