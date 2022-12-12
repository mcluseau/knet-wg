package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
)

var (
	nftEnabled = flag.Bool("nft", false, "enable NFT basic packet filter (pods SNAT...)")
	nftMasqOif = flag.String("nft-masq-oif", "eth0", "output interface match to masquerade (ie: \"eth0\", \"{eth0, eth1}\", etc)")
)

func setupNFT(podCIDRs []string) {
	if !*nftEnabled {
		return
	}

	var err error
	defer func() {
		if err != nil {
			log.Print("nft failed: ", err)
		}
	}()

	nft := exec.Command("nft", "-f", "-")
	nft.Stdout = os.Stdout
	nft.Stderr = os.Stderr

	nftIn, err := nft.StdinPipe()
	if err != nil {
		return
	}

	err = nft.Start()
	if err != nil {
		return
	}

	write := func(s string) {
		if err != nil {
			return
		}

		_, err = nftIn.Write([]byte(s))
	}

	write(`
table ip knet_wg;
delete table ip knet_wg;
table ip knet_wg {
  chain hook_postrouting {
    type nat hook postrouting priority 999;
    fib saddr type local oif "` + *ifName + `" masquerade;
`)

	for _, cidr := range podCIDRs {
		write("    ip saddr " + cidr + " oif " + *nftMasqOif + " masquerade;\n")
	}

	write("  }\n}")

	nftIn.Close()
	err = nft.Wait()
}
