from mcluseau/golang-builder:1.19.4 as build

from alpine:3.17
run apk add --update --no-cache wireguard-tools iproute2 nftables

entrypoint ["/bin/knet-wg"]
copy --from=build /go/bin/* /bin/
