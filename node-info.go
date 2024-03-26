package main

import "strconv"

func (ni nodeInfo) EndpointFrom(net string) string {
	if ep, ok := ni.Endpoints[net]; ok {
		return ep
	} else if ni.Endpoint != "" {
		return ni.Endpoint
	} else {
		return ni.IP + ":" + strconv.Itoa(int(ni.ListenPort))
	}
}
