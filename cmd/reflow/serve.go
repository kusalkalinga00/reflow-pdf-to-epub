package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/convert"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/ingest"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/web"
)

func serveCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, `usage: reflow serve [-addr host:port]

Start the local conversion UI. It binds to localhost by default and is not
meant to be exposed to a network.
`)
	}

	addr := fs.String("addr", "127.0.0.1:8080", "address to listen on")
	maxMB := fs.Int("max-mb", int(ingest.DefaultLimits.MaxBytes>>20), "largest accepted upload in MB")
	concurrency := fs.Int("jobs", 2, "conversions allowed at once")

	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return exitUsage
	}

	converter, err := convert.New(convert.Options{})
	if err != nil {
		fmt.Fprintln(stderr, "reflow:", err)
		return exitFailure
	}
	defer converter.Close()

	server := web.New(converter, web.Options{
		MaxUpload:   int64(*maxMB) << 20,
		Concurrency: *concurrency,
	})

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: server.Handler(),
		// A conversion of a large book can take minutes; the write timeout has
		// to allow for it, while idle connections are still reaped.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      15 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(stderr, "reflow:", err)
		return exitFailure
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdown)
	}()

	fmt.Fprintf(stdout, "reflow is listening on http://%s\n", listener.Addr())
	fmt.Fprintln(stdout, "press ctrl-c to stop")

	if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(stderr, "reflow:", err)
		return exitFailure
	}

	fmt.Fprintln(stdout, "stopped")
	return exitOK
}
