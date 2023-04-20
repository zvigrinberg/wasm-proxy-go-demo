module github.com/zvigrinberg/wasm-proxy-go-demo

go 1.19

replace github.com/zvigrinberg/wasm-proxy-go-demo => ./

require github.com/tetratelabs/proxy-wasm-go-sdk v0.21.0

require github.com/stretchr/testify v1.8.2 // indirect

require github.com/CosmWasm/tinyjson v0.9.0