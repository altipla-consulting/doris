
# doris

[![Go Reference](https://pkg.go.dev/badge/github.com/altipla-consulting/doris.svg)](https://pkg.go.dev/github.com/altipla-consulting/doris)

HTTP server for Connect APIs and websites.


## Install

```shell
go get github.com/altipla-consulting/doris
```


## Register services

Each API package receives the Doris server and mounts its service with its own
interceptors. `MountAPI` creates a separate Connect server for that mount.
Doris always runs its standard timeout, request trimming, and error reporting
interceptors before the interceptors passed to `MountAPI`, and those extra
interceptors apply only to the services registered by the mount function.

For example, after generating Connect v2 bindings and implementing the service
and the `auth.STS` server interceptor:

```go
func RegisterServer(server *doris.Server) {
    server.MountAPI(func(s *connect.Server) {
        engine_dataconnect.RegisterEngineDataServiceHandler(s, NewServer())
    }, auth.STS("engine_data.read"))
}
```

An API without additional interceptors simply omits the second argument to
`MountAPI`. Application startup calls each package's registration function:

```go
server := doris.NewServer()
enginedata.RegisterServer(server)
server.Serve()
```

## Contributing

You can make pull requests or create issues in GitHub. Any code you send should be formatted using `make gofmt`.


## License

[MIT License](LICENSE)
