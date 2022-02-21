/*
 * Copyright 2022 OpsMx, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License")
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/skandragon/gohealthcheck/health"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	// eg, http://localhost:14268/api/traces
	jaegerEndpoint = flag.String("jaeger-endpoint", "", "Jaeger collector endpoint")

	healthchecker = health.MakeHealth()
	tracer        trace.Tracer
	hostname      string
)

func loggingMiddleware(next http.Handler) http.Handler {
	return handlers.LoggingHandler(os.Stdout, next)
}

func getEnvar(name string, defaultValue string) string {
	value, found := os.LookupEnv(name)
	if !found {
		return defaultValue
	}
	return value
}

func gitBranch() string {
	return getEnvar("GIT_BRANCH", "dev")
}

func gitHash() string {
	return getEnvar("GIT_HASH", "dev")
}

func showGitInfo() {
	log.Printf("GIT Version: %s @ %s", gitBranch(), gitHash())
}

type srv struct {
	listenPort uint16
}

type rootResponse struct {
	Now       int64       `json:"now,omitempty"`
	URI       string      `json:"uri,omitempty"`
	Headers   http.Header `json:"headers,omitempty"`
	Hostname  string      `json:"hostname,omitempty"`
	GitHash   string      `json:"git_hash,omitempty"`
	GitBranch string      `json:"git_branch,omitempty"`
}

func handleRoot(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("content-type", "application/json")

	ret := rootResponse{
		Now:       time.Now().UnixMicro(),
		URI:       req.RequestURI,
		Headers:   req.Header,
		Hostname:  hostname,
		GitHash:   gitHash(),
		GitBranch: gitBranch(),
	}
	j, err := json.Marshal(ret)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.Write(j)
}

type handleRandomErrorResponse struct {
	Now     int64   `json:"now"`
	Chance  float64 `json:"chance"`
	Point   float64 `json:"point"`
	Message string  `json:"message"`
}

func handleRandomError(w http.ResponseWriter, req *http.Request) {
	chance := req.FormValue("chance")
	if chance == "" {
		chance = "0.25"
	}
	chanceVal, err := strconv.ParseFloat(chance, 64)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	point := rand.Float64()

	ret := handleRandomErrorResponse{
		Now:    time.Now().UnixMicro(),
		Chance: chanceVal,
		Point:  point,
	}

	var code int
	if chanceVal > point {
		code = http.StatusInternalServerError
		ret.Message = "Random failure!"
	} else {
		code = http.StatusOK
		ret.Message = "Success!"
	}

	j, err := json.Marshal(ret)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(code)
	w.Write(j)
}

func (s *srv) routes(r *mux.Router) {
	r.Path("/randomResult").HandlerFunc(handleRandomError).Methods(http.MethodGet)
}

func runHTTPServer(ctx context.Context, healthchecker *health.Health) {
	s := &srv{
		listenPort: 8000,
	}
	r := mux.NewRouter()

	// Order matters.
	r.HandleFunc("/health", healthchecker.HTTPHandler()).Methods(http.MethodGet)
	s.routes(r)
	r.PathPrefix("/").HandlerFunc(handleRoot).Methods(http.MethodGet)

	r.Use(loggingMiddleware)
	r.Use(otelmux.Middleware("go-demo-web"))

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.listenPort),
		Handler: r,
	}
	log.Fatal(srv.ListenAndServe())

}

func newTracerProvider(url string, githash string) (*tracesdk.TracerProvider, error) {
	opts := []tracesdk.TracerProviderOption{
		tracesdk.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("go-demo-web"),
			semconv.ServiceVersionKey.String("1.0.0"),
		)),
		tracesdk.WithSampler(tracesdk.AlwaysSample()),
	}

	if url != "" {
		exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(url)))
		if err != nil {
			return nil, err
		}
		opts = append(opts, tracesdk.WithBatcher(exp))
	}
	tp := tracesdk.NewTracerProvider(opts...)
	return tp, nil
}

func main() {
	showGitInfo()

	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGTERM, syscall.SIGINT)

	flag.Parse()
	if len(*jaegerEndpoint) == 0 {
		*jaegerEndpoint = getEnvar("JAEGER_TRACE_URL", "")
	}

	tracerProvider, err := newTracerProvider(*jaegerEndpoint, gitHash())
	if err != nil {
		log.Fatal(err)
	}
	otel.SetTracerProvider(tracerProvider)
	tracer = tracerProvider.Tracer("main")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func(ctx context.Context) {
		ctx, cancel = context.WithTimeout(ctx, time.Second*5)
		defer cancel()
		if err := tracerProvider.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}
	}(ctx)

	hostname, err = os.Hostname()
	if err != nil {
		log.Fatal(err)
	}

	go healthchecker.RunCheckers(15)

	go runHTTPServer(ctx, healthchecker)

	<-sigchan
	log.Printf("Exiting Cleanly")
}
