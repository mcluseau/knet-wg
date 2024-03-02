from mcluseau/golang-builder:1.21.6 as build

from alpine:3.19.1
run apk add --update --no-cache wireguard-tools iproute2 nftables

entrypoint ["/bin/knet-wg"]
copy --from=build /go/bin/* /bin/
