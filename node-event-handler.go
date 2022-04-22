package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"text/template"

	"github.com/cespare/xxhash"
	v1 "k8s.io/api/core/v1"
)

const pubkeyAnnotation = "kwg-pubkey"

type nodeEventHandler struct {
	hs    map[string]uint64
	nodes map[string]nodeInfo
}

type nodeInfo struct {
	Name     string
	IP       string
	IPs      []string
	PodCIDRs []string
	AllCIDRs []string
	PubKey   string
}

func (neh *nodeEventHandler) OnAdd(obj interface{}) {
	node := obj.(*v1.Node)

	ni := nodeInfo{
		Name:     node.Name,
		PubKey:   node.Annotations[pubkeyAnnotation],
		PodCIDRs: node.Spec.PodCIDRs,
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

func (neh *nodeEventHandler) OnUpdate(oldObj, newObj interface{}) {
	neh.OnAdd(newObj)
}

func (neh *nodeEventHandler) OnDelete(obj interface{}) {
	node := obj.(*v1.Node)

	delete(neh.hs, node.Name)
	delete(neh.nodes, node.Name)

	neh.updateWG()
}

func (neh *nodeEventHandler) updateWG() {
	cfg := &bytes.Buffer{}

	privateKey, err := ioutil.ReadFile(*keyPath)
	if err != nil {
		log.Fatal("failed to read private key: ", err)
	}

	data := struct {
		Node       nodeInfo
		PrivateKey string
		Peers      []nodeInfo
	}{
		Node:       neh.nodes[*nodeName],
		PrivateKey: string(bytes.TrimSpace(privateKey)),
		Peers:      make([]nodeInfo, 0, len(neh.nodes)-1),
	}

	for _, node := range neh.nodes {
		if node.Name == *nodeName {
			continue
		}

		data.Peers = append(data.Peers, node)
	}

	sort.Slice(data.Peers, func(i, j int) bool {
		return data.Peers[i].Name < data.Peers[j].Name
	})

	tmpl.Execute(cfg, data)

	log.Print("new config")

	os.MkdirAll(filepath.Dir(*cfgPath), 0755)
	err = ioutil.WriteFile(*cfgPath, cfg.Bytes(), 0600)
	if err != nil {
		log.Fatal("failed to write config: ", err)
	}

	err = run("wg", "syncconf", ifName, *cfgPath)
	if err != nil {
		log.Fatal("wg syncconf failed: ", err)
	}

	// sync IPs
	if node, nodeFound := neh.nodes[*nodeName]; nodeFound {
		iface, _ := net.InterfaceByName(ifName)
		if iface != nil {
			syncNodeIPs(node.PodCIDRs, iface)
		}

		setupNFT(node.PodCIDRs)
	}

	// check routes exist
	for _, node := range neh.nodes {
		if node.Name == *nodeName {
			continue
		}
		for _, cidrString := range node.PodCIDRs {
			// todo: check the need to add

			ip, _, _ := net.ParseCIDR(cidrString)
			chkOut, _ := exec.Command("ip", "route", "get", ip.String()).Output()
			if !bytes.Contains(chkOut, []byte(" dev "+ifName)) {
				log.Print("adding ip route to ", cidrString)
				run("ip", "route", "add", cidrString, "dev", ifName)
			}
		}
	}
}

var tmpl = template.Must(template.New("wgcfg").Parse(`[Interface]
PrivateKey = {{.PrivateKey}}
ListenPort = 51820
{{ range .Peers }}{{ if .PubKey }}
[Peer]
# Name: {{.Name}}
PublicKey = {{.PubKey}}
Endpoint = {{.IP}}:51820
AllowedIPs = {{ range $i, $ip := .PodCIDRs }}{{ if gt $i 0 }}, {{ end }}{{ $ip }}{{ end }}
{{ end }}{{ end }}
`))
