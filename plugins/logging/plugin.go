package logging

import (
	"context"

	plugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	"github.com/hashicorp/nomad/plugins/logging/proto"
)

// LoggingPlugin is the interface which logging plugins will implement. It is
// also implemented by a plugin client which proxies the calls to go-plugin. See
// the proto/logging.proto file for detailed information about each RPC and
// message structure.
type LoggingPlugin interface {
	Start(*LogConfig) error
	Stop() error
	Capabilities(context.Context) (*Capabilities, error)
}

type LogConfig struct {
	// LogDir is the host path where logs are to be written to
	LogDir string

	// StdoutLogFile is the path relative to LogDir for stdout logging
	StdoutLogFile string

	// StderrLogFile is the path relative to LogDir for stderr logging
	StderrLogFile string

	// StdoutFifo is the path on the host to the stdout pipe
	StdoutFifo string

	// StderrFifo is the path on the host to the stderr pipe
	StderrFifo string

	// MaxFiles is the max rotated files allowed
	MaxFiles int

	// MaxFileSizeMB is the max log file size in MB allowed before rotation occures
	MaxFileSizeMB int
}

// TODO
type Capabilities struct{}

type Plugin struct {
	plugin.NetRPCUnsupportedPlugin
	impl LoggingPlugin
}

// NewPlugin should be called when the plugin's main function calls
// go-plugin.Serve; the plugin main will pass in the concrete implementation of
// the LoggingPlugin interface.
func NewPlugin(i LoggingPlugin) plugin.Plugin {
	return &Plugin{impl: i}
}

// GRPCServer is needed for the go-plugin interface
func (p *Plugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterLoggingPluginServer(s, &loggingServer{
		impl:   p.impl,
		broker: broker,
	})
	return nil
}

// GRPCClient is needed for the go-plugin interface
func (p *Plugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &loggingClient{
		doneCtx: ctx,
		client:  proto.NewLoggingPluginClient(c),
	}, nil
}
