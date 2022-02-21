# go-demo-web
A simple web server, providing some simple API endpoints with randomized failure rates.

# Usage

```
make && ./bin/go-demo-web

curl localhost:8000/foo
```

# Endpoints

`/health` returns the server health as a JSON object and 200 status code when healthy.

`/randomResult` returns a random result, with a query parameter called "chance" that can be from 0.0 to 1.0 to indicate the chance of an error result.  It will return status 200 or 500 for success or failure.

Any other path will return a JSON result with some data copied from the request, and some internal housekeeping info like the hostname, git hash and version, etc.

# Jaeger tracing

If the command-line argument `--jaeger-endpoint` or the environment variable `JAEGER_TRACE_URL`
is set to a [Jeager](https://www.jaegertracing.io/) trace endpoint, HTTP activity will generate traces.

Example:
```
./bin/go-demo-web --jaeger-endpoint http://localhost:14268/api/traces
```
