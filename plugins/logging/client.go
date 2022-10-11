package logging

import (
	"context"
	"time"

	"github.com/hashicorp/nomad/helper/pluginutils/grpcutils"
	"github.com/hashicorp/nomad/plugins/logging/proto"
)

type loggingClient struct {
	client proto.LoggingPluginClient

	// doneCtx is closed when the plugin exits
	doneCtx context.Context
}

const loggingRPCTimeout = 1 * time.Minute

func (c *loggingClient) Start(cfg *LogConfig) error {
	req := &proto.StartRequest{
		LogDir:         cfg.LogDir,
		StdoutFileName: cfg.StdoutLogFile,
		StderrFileName: cfg.StderrLogFile,
		MaxFiles:       uint32(cfg.MaxFiles),
		MaxFileSizeMb:  uint32(cfg.MaxFileSizeMB),
		StdoutFifo:     cfg.StdoutFifo,
		StderrFifo:     cfg.StderrFifo,
	}
	ctx, cancel := context.WithTimeout(context.Background(), loggingRPCTimeout)
	defer cancel()

	_, err := c.client.Start(ctx, req)
	return grpcutils.HandleGrpcErr(err, c.doneCtx)
}

func (c *loggingClient) Stop() error {
	req := &proto.StopRequest{}
	ctx, cancel := context.WithTimeout(context.Background(), loggingRPCTimeout)
	defer cancel()

	_, err := c.client.Stop(ctx, req)
	return grpcutils.HandleGrpcErr(err, c.doneCtx)
}

func (c *loggingClient) Capabilities() (*Capabilities, error) {
	req := &proto.CapabilitiesRequest{}
	ctx, cancel := context.WithTimeout(context.Background(), loggingRPCTimeout)
	defer cancel()

	_, err := c.client.Capabilities(ctx, req)
	return &Capabilities{}, err
}
