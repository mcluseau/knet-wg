package main

import (
	"strings"

	v1 "k8s.io/api/core/v1"
)

const (
	annPrefix       = "kwg-"
	annListenPort   = annPrefix + "listen-port"
	annPubkey       = annPrefix + "pubkey"
	annNet          = annPrefix + "net"
	annEndpointFrom = annPrefix + "endpoint-from/"
)

func annotation(node *v1.Node, name string) string {
	if node == nil || node.Annotations == nil {
		return ""
	}

	return node.Annotations[name]
}

func annotationsByPrefix(node *v1.Node, prefix string) (anns map[string]string) {
	anns = map[string]string{}

	if node == nil || node.Annotations == nil {
		return
	}

	for k, v := range node.Annotations {
		k, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		anns[k] = v
	}

	return
}
