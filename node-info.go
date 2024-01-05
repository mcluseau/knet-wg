package main

func (ni nodeInfo) EndpointFrom(net string) string {
	if ep, ok := ni.Endpoints[net]; ok {
		return ep
	} else {
		return ni.IP + ":51820"
	}
}
