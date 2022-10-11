package logging

import (
	"context"

	"github.com/hashicorp/go-plugin"

	"github.com/hashicorp/nomad/plugins/logging/proto"
)

type loggingServer struct {
	broker *plugin.GRPCBroker
	impl   LoggingPlugin
}

func (s *loggingServer) Start(ctx context.Context, req *proto.StartRequest) (*proto.StartResponse, error) {
	cfg := &LogConfig{
		LogDir:        req.LogDir,
		StdoutLogFile: req.StdoutFileName,
		StderrLogFile: req.StderrFileName,
		MaxFiles:      int(req.MaxFiles),
		MaxFileSizeMB: int(req.MaxFileSizeMb),
		StdoutFifo:    req.StdoutFifo,
		StderrFifo:    req.StderrFifo,
	}

	err := s.impl.Start(cfg)
	if err != nil {
		return nil, err
	}
	resp := &proto.StartResponse{}
	return resp, nil
}

func (s *loggingServer) Stop(ctx context.Context, req *proto.StopRequest) (*proto.StopResponse, error) {
	return &proto.StopResponse{}, s.impl.Stop()
}

func (b *loggingServer) Capabilities(ctx context.Context, req *proto.CapabilitiesRequest) (*proto.CapabilitiesResponse, error) {
	_, err := b.impl.Capabilities(ctx)

	// TODO: map the capabilities to the response
	return &proto.CapabilitiesResponse{}, err
}
