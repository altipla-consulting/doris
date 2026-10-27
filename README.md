
# doris

[![Go Reference](https://pkg.go.dev/badge/github.com/altipla-consulting/doris.svg)](https://pkg.go.dev/github.com/altipla-consulting/doris)

HTTP server for Connect APIs and websites.


## Install

```shell
go get github.com/altipla-consulting/doris
```


## Register services

Each API package receives the Connect hub and mounts its service with its own
interceptors. Doris creates a separate Connect server for each mount and always
adds its standard timeout, request trimming, and error reporting interceptors
before the service interceptors.

For example, after generating Connect v2 bindings and implementing the service
and the `auth.STS` server interceptor:

```go
func RegisterServer(hub *doris.ConnectHub) {
    hub.Mount(func(server *connect.Server) {
        engine_dataconnect.RegisterEngineDataServiceHandler(server, NewServer())
    }, auth.STS("engine_data.read"))
}
```

An API without additional interceptors simply omits the second argument to
`hub.Mount`. Application startup calls each package's registration function:

```go
server := doris.NewServer()
hub := doris.NewConnectHub(server.Router)
enginedata.RegisterServer(hub)
server.Serve()
```

## Contributing

You can make pull requests or create issues in GitHub. Any code you send should be formatted using `make gofmt`.


## License

[MIT License](LICENSE)
