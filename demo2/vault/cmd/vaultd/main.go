package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/time/rate"

	ratelimitkit "github.com/go-kit/kit/ratelimit"

	"context"

	"github.com/shan-xi/gomicroservice/demo2/vault"
	"github.com/shan-xi/gomicroservice/demo2/vault/pb"
	"google.golang.org/grpc"
)

func main() {
	var (
		httpAddr = flag.String("http", ":8080", "http listen address")
		gRPCAddr = flag.String("grpc", ":8081", "gRPC listen address")
	)
	flag.Parse()
	ctx := context.Background()
	srv := vault.NewService()
	errChan := make(chan error)
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errChan <- fmt.Errorf("%s", <-c)
	}()

	var (
		qps = 100 // beyond which we will return an error
		// maxAttempts = 3                      // per request, before giving up
		// maxTime     = 250 * time.Millisecond // wallclock time, before giving up
	)

	// rlbucket := ratelimit.NewBucket(1*time.Second, 5)
	hashEndpoint := vault.MakeHashEndpoint(srv)
	{
		hashEndpoint = ratelimitkit.NewErroringLimiter(rate.NewLimiter(rate.Every(time.Second), qps))(hashEndpoint)
	}
	validateEndpoint := vault.MakeValidateEndpoint(srv)
	{
		validateEndpoint = ratelimitkit.NewErroringLimiter(rate.NewLimiter(rate.Every(time.Second), qps))(validateEndpoint)
	}
	endpoints := vault.Endpoints{
		HashEndpoint:     hashEndpoint,
		ValidateEndpoint: validateEndpoint,
	}

	// HTTP transport
	go func() {
		log.Println("http:", *httpAddr)
		handler := vault.NewHTTPServer(ctx, endpoints)
		errChan <- http.ListenAndServe(*httpAddr, handler)
	}()

	// gRPC transport
	go func() {
		listener, err := net.Listen("tcp", *gRPCAddr)
		if err != nil {
			errChan <- err
			return
		}
		log.Println("grpc:", *gRPCAddr)
		handler := vault.NewGRPCServer(ctx, endpoints)
		gRPCServer := grpc.NewServer()
		pb.RegisterVaultServer(gRPCServer, handler)
		errChan <- gRPCServer.Serve(listener)
	}()

	log.Fatalln(<-errChan)
}
