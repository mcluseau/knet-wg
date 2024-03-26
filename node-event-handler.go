package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"text/template"

	"github.com/cespare/xxhash"
	v1 "k8s.io/api/core/v1"
)

const defaultListenPort = 51820

type nodeEventHandler struct {
	hs    map[string]uint64
	nodes map[string]nodeInfo
}

type nodeInfo struct {
	Name       string
	ListenPort uint16
	Net        string
	IP         string
	IPs        []string
	PodCIDRs   []string
	AllCIDRs   []string
	PubKey     string
	Endpoint   string
	Endpoints  map[string]string
}

func (neh *nodeEventHandler) OnAdd(obj any) {
	node := obj.(*v1.Node)

	ni := nodeInfo{
		Name:       node.Name,
		ListenPort: defaultListenPort,
		Net:        annotation(node, annNet),
		PubKey:     annotation(node, annPubkey),
		PodCIDRs:   node.Spec.PodCIDRs,
		Endpoint:   annotation(node, annEndpoint),
		Endpoints:  annotationsByPrefix(node, annEndpointFrom),
	}

	if portStr := annotation(node, annListenPort); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 0xffff {
			log.Printf("invalid port %q on node %q", portStr, node.Name)
		} else {
			ni.ListenPort = uint16(port)
		}
	}

	if ni.Net == "" {
		ni.Net = "default"
	}

	ni.AllCIDRs = append(ni.AllCIDRs, ni.PodCIDRs...)

	for _, addr := range node.Status.Addresses {
		ip := net.ParseIP(addr.Address)
		if ip == nil {
			continue
		}

		if ni.IP == "" {
			ni.IP = addr.Address
		}

		ni.IPs = append(ni.IPs, addr.Address)
		ni.AllCIDRs = append(ni.AllCIDRs, addr.Address+"/32")
	}

	h := xxhash.New()
	json.NewEncoder(h).Encode(ni)
	sum := h.Sum64()

	if neh.hs[node.Name] == sum {
		return
	}

	neh.hs[node.Name] = sum
	neh.nodes[node.Name] = ni

	neh.updateWG()
}

func (neh *nodeEventHandler) OnUpdate(oldObj, newObj any) {
	neh.OnAdd(newObj)
}

func (neh *nodeEventHandler) OnDelete(obj any) {
	node := obj.(*v1.Node)

	delete(neh.hs, node.Name)
	delete(neh.nodes, node.Name)

	neh.updateWG()
}

func (neh *nodeEventHandler) updateWG() {
	cfg := &bytes.Buffer{}

	privateKey, err := os.ReadFile(*keyPath)
	if err != nil {
		log.Fatal("failed to read private key: ", err)
	}

	data := struct {
		Node       nodeInfo
		PrivateKey string
		ListenPort uint16
		Peers      []nodeInfo
	}{
		Node:       neh.nodes[*nodeName],
		PrivateKey: string(bytes.TrimSpace(privateKey)),
		ListenPort: defaultListenPort,
		Peers:      make([]nodeInfo, 0, len(neh.nodes)-1),
	}

	for _, node := range neh.nodes {
		if node.Name == *nodeName {
			data.ListenPort = node.ListenPort
			continue
		}

		data.Peers = append(data.Peers, node)
	}

	sort.Slice(data.Peers, func(i, j int) bool {
		return data.Peers[i].Name < data.Peers[j].Name
	})

	if err := tmpl.Execute(cfg, data); err != nil {
		log.Fatal("failed to generate config: ", err)
	}

	log.Printf("new config with %d peers", len(data.Peers))

	os.MkdirAll(filepath.Dir(*cfgPath), 0755)
	err = os.WriteFile(*cfgPath, cfg.Bytes(), 0600)
	if err != nil {
		log.Fatal("failed to write config: ", err)
	}

	err = run("wg", "syncconf", *ifName, *cfgPath)
	if err != nil {
		log.Fatal("wg syncconf failed: ", err)
	}

	// sync IPs
	iface, _ := net.InterfaceByName(*ifName)
	if iface != nil {
		syncNodeIPs(data.Node.PodCIDRs, iface)
	}

	setupNFT(data.Node.PodCIDRs)

	// check routes exist
	for _, node := range neh.nodes {
		if node.Name == *nodeName {
			continue
		}
		for _, cidrString := range node.PodCIDRs {
			// todo: check the need to add

			ip, _, _ := net.ParseCIDR(cidrString)
			chkOut, _ := exec.Command("ip", "route", "get", ip.String()).Output()
			if !bytes.Contains(chkOut, []byte(" dev "+*ifName)) {
				log.Print("adding ip route to ", cidrString)
				run("ip", "route", "add", cidrString, "dev", *ifName)
			}
		}
	}
}

var tmpl = template.Must(template.New("wgcfg").Parse(`[Interface]
PrivateKey = {{.PrivateKey}}
ListenPort = {{.ListenPort}}
{{ $node := .Node }}
{{ range .Peers }}{{ if .PubKey }}
[Peer]
# Name: {{ .Name }}
PublicKey = {{ .PubKey }}
Endpoint = {{ .EndpointFrom $node.Net }}
AllowedIPs = {{ range $i, $ip := .PodCIDRs }}{{ if gt $i 0 }}, {{ end }}{{ $ip }}{{ end }}
{{ end }}{{ end }}
`))
