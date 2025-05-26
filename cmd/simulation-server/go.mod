module simulation-server

go 1.23

toolchain go1.23.9

require (
	github.com/gorilla/websocket v1.5.3
	simulation v0.0.0
)

replace simulation => ../../
