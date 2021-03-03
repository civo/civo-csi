package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/civo/civo-csi/internal/driver"

	log "github.com/sirupsen/logrus"
)

func main() {
	d, err := driver.NewDriver(os.Getenv("CIVO_API_URL"), os.Getenv("CIVO_API_KEY"), os.Getenv("CIVO_REGION"))
	if err != nil {
		log.Fatalln(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
	}()

	if err := d.Run(ctx); err != nil {
		log.Fatalln(err)
	}
}
