# WASM Proxy GO Demo

## Prerequisites 

1. Need to download [TinyGo](https://tinygo.org/)
2. Need to download [Envoy Proxy](https://www.envoyproxy.io/docs/envoy/latest/start/install) in order to quickly test plugin - you can also do it easily using [func-e.io](https://func-e.io/).

## Procedure

1. Compile plugin Go source code to create a WebAssembley Binary:
```shell
tinygo build -o plugin.wasm -scheduler=none -target=wasi ./main.go
```

2. Run Mock Microservice Using Wiremock Mock Web Server
```shell
podman run -d -p 9999:9999 --name mock-service quay.io/zgrinber/wiremock:latest java -jar /var/wiremock/lib/wiremock-jre8-standalone.jar --port 9999
```

3. Add Stub Mapping to mock /employees POST Endpoint:
```shell
curl -i -X POST http://localhost:9999/__admin/mappings/import -T mappings.json
```

4. Populate environment variables with relevant values:
```shell
export CLINET_ID=postman
export CLIENT_SECRET=changeme
export API_KEY=changeme
export MANIFEST_NAME=Employee
export JOB_TYPE=Restrict
export RESTRICTED_TEXT=*********
export PROTECT_NULL_VALUES=false
export PRESERVE_STRING_LENGTH=true
export CLIENT_ID=postman
export INTERCEPTOR_CLUSTER_NAME=interceptor_service
```

5. Now run the wasm proxy plugin using envoy:
```shell
envoy -c envoy.yaml  --concurrency 2 --log-format '%v'
```

6. Test the plugin by sending HTTP Post Request 
```shell
curl -i --location --request POST 'http://localhost:18000/employees' --header 'Content-Type: application/json' --header 'Cookie: cd10b69e39387eb7ec9ac241201ab1ab=fcd79ca747647ba06fa611fdf057fb80' --data-raw '{"countryCode": "IL", "dataOwningCountryCode": "IL"}'
```

7. Build Container Image with the Wasm Binary:
```shell
podman build -t quay.io/zgrinber/golang-wasm-plugin:latest .
```

8. Push Image to your registry using your credentials:
```shell
podman push quay.io/zgrinber/golang-wasm-plugin:latest
```