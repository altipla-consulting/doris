
# doris

[![Go Reference](https://pkg.go.dev/badge/github.com/altipla-consulting/doris.svg)](https://pkg.go.dev/github.com/altipla-consulting/doris)

HTTP server for Connect APIs and websites.


## Install

```shell
go get github.com/altipla-consulting/doris
```


## Register services

Each API package receives the router and mounts its service with its own
interceptors. `Router.MountAPI` creates a separate Connect server for that
mount. Doris always runs its standard timeout, request trimming, and error
reporting interceptors before the interceptors passed to `MountAPI`, and those
extra interceptors apply only to the services registered by the mount function.

For example, after generating Connect v2 bindings and implementing the service
and the `auth.STS` server interceptor:

```go
func RegisterServer(router *doris.Router) {
    router.MountAPI(func(server *connect.Server) {
        engine_dataconnect.RegisterEngineDataServiceHandler(server, NewServer())
    }, auth.STS("engine_data.read"))
}
```

An API without additional interceptors simply omits the second argument to
`MountAPI`. Application startup calls each package's registration function:

```go
server := doris.NewServer()
enginedata.RegisterServer(server.Router)
server.Serve()
```

## Contributing

You can make pull requests or create issues in GitHub. Any code you send should be formatted using `make gofmt`.


## License

[MIT License](LICENSE)
