package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var (
	ifName  = flag.String("ifname", "kwg", "Wireguard interface name")
	keyPath = flag.String("key-path", "wg.key", "path to the private key")
	cfgPath = flag.String("config", "/etc/wireguard/kwg.cfg", "WireGuard configuration to write")

	kubeconfig = flag.String("kubeconfig", os.Getenv("KUBECONFIG"), "path to the kube-config")
	master     = flag.String("master", "", "Master if not using the default master")
	nodeName   = flag.String("node-name", func() string {
		s, _ := os.Hostname()
		return s
	}(), "node name")

	k           *kubernetes.Clientset
	ctx, cancel = context.WithCancel(context.Background())
	stopCh      = make(chan struct{}, 1)
)

func main() {
	flag.Parse()

	log.Print("knet-wg starting")

	err := connect()
	if err != nil {
		log.Fatal(err)
	}

	var key wgtypes.Key
	{ // ensure we have a key
		keyData, err := ioutil.ReadFile(*keyPath)
		if err == nil {
			key, err = wgtypes.ParseKey(string(keyData))
			if err != nil {
				log.Fatal(err)
			}
		} else if os.IsNotExist(err) {
			key, err = wgtypes.GeneratePrivateKey()
			if err != nil {
				log.Fatal(err)
			}

			err = ioutil.WriteFile(*keyPath, []byte(key.String()), 0600)
			if err != nil {
				log.Fatal(err)
			}
		} else {
			log.Fatal(err)
		}
	}

	// ------------------------------------------------------------------------

	node, err := k.CoreV1().Nodes().Get(ctx, *nodeName, metav1.GetOptions{})
	if err != nil {
		log.Fatal("failed to get node ", *nodeName, ": ", err)
	}

	{ // ensure the node has published its key
		if node.Annotations[pubkeyAnnotation] != key.PublicKey().String() {
			log.Print("setting our pubkey annotation to node")
			node.Annotations[pubkeyAnnotation] = key.PublicKey().String()
			_, err = k.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	// ------------------------------------------------------------------------

	iface, _ := net.InterfaceByName(*ifName)
	{ // create the kwg interface
		// err is not usable here, only an internal value is set in OpError.Err

		if iface == nil {
			log.Print("creating interface ", *ifName)
			_ = run("ip", "link", "add", *ifName, "type", "wireguard")
			iface, _ = net.InterfaceByName(*ifName)
		}

		err = run("ip", "link", "set", *ifName, "up")
		if err != nil {
			log.Fatal("failed to set link up: ", err)
		}

		syncNodeIPs(node.Spec.PodCIDRs, iface)
	}

	// ------------------------------------------------------------------------

	writeCNIConfig(node, iface)
	setupNFT(node.Spec.PodCIDRs)

	// ------------------------------------------------------------------------

	factory := informers.NewSharedInformerFactory(k, time.Second*30)
	factory.Start(stopCh)

	coreFactory := factory.Core().V1()

	nodesInformer := coreFactory.Nodes().Informer()
	nodesInformer.AddEventHandler(&nodeEventHandler{
		hs:    map[string]uint64{},
		nodes: map[string]nodeInfo{},
	})
	nodesInformer.Run(stopCh)
}

func connect() (err error) {
	cfg, err := clientcmd.BuildConfigFromFlags(*master, *kubeconfig)
	if err != nil {
		err = fmt.Errorf("failed to build Kubernetes config: %w", err)
		return
	}

	c, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		err = fmt.Errorf("failed to build Kubernetes client: %w", err)
		return
	}

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

		<-c
		log.Print("interrupted, cancelling operations...")
		cancel()
		close(stopCh)

		<-c
		log.Print("second interrupt, exiting now.")
		os.Exit(1)
	}()

	k = c
	return
}
