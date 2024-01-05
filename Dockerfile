from mcluseau/golang-builder:1.21.4 as build

from alpine:3.18.4
run apk add --update --no-cache wireguard-tools iproute2 nftables

entrypoint ["/bin/knet-wg"]
copy --from=build /go/bin/* /bin/
