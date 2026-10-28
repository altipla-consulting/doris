
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
    doris.MountAPI(server, func(s *connect.Server) {
        engine_dataconnect.RegisterEngineDataServiceHandler(s, NewServer())
    }, auth.STS("engine_data.read"))
}
```

An API without additional interceptors simply omits the third argument to
`MountAPI`. Application startup calls each package's registration function:

```go
server := doris.NewServer()
enginedata.RegisterServer(server)
server.Serve()
```

## CORS for Connect APIs

Create a subrouter, attach the CORS middleware to it, and pass that subrouter to
`MountAPI`. `ConnectCORS` provides the allowed methods and headers for Connect;
pass the browser origins that should be allowed to call the API.

```go
import "github.com/rs/cors"

func RegisterServer(server *doris.Server) {
    sub := server.Subrouter()
    sub.Use(cors.New(doris.ConnectCORS([]string{"https://example.com"})).Handler)

    doris.MountAPI(sub, func(s *connect.Server) {
        engine_dataconnect.RegisterEngineDataServiceHandler(s, NewServer())
    }, auth.STS("engine_data.read"))
}
```

The subrouter inherits the server middleware and applies CORS only to the APIs
mounted on it. Other routes registered on the server keep their own behavior.
Leave the subrouter unrestricted by HTTP method so the CORS middleware can
handle browser `OPTIONS` preflight requests as well as API calls.

## Contributing

You can make pull requests or create issues in GitHub. Any code you send should be formatted using `make gofmt`.


## License

[MIT License](LICENSE)
